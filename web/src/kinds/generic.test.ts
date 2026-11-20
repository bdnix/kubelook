import { describe, expect, it } from 'vitest';
import { crdDescriptor, crdName, type CrdLike } from './generic';

const crd: CrdLike = {
  spec: {
    group: 'example.com',
    names: { kind: 'Widget', plural: 'widgets' },
    scope: 'Namespaced',
    versions: [
      {
        name: 'v1',
        served: true,
        additionalPrinterColumns: [
          { name: 'State', type: 'string', jsonPath: '.status.state' },
          { name: 'Replicas', type: 'integer', jsonPath: '.spec.replicas' },
          { name: 'Since', type: 'date', jsonPath: '.status.since' },
          { name: 'Ratio', type: 'number', jsonPath: '.spec.ratio' },
        ],
      },
      { name: 'v2', served: true },
    ],
  },
};

const widget = {
  metadata: { name: 'w', namespace: 'ns', creationTimestamp: '2026-01-01T00:00:00Z' },
  spec: { replicas: 3, ratio: 0.5 },
  status: { state: 'Ready', since: '2026-01-01T00:00:00Z' },
};
const now = Date.parse('2026-01-01T01:00:00Z');

describe('crdName', () => {
  it('is plural.group', () => expect(crdName('example.com', 'widgets')).toBe('widgets.example.com'));
});

describe('crdDescriptor', () => {
  it('has Name, Namespace, the printer columns of the version, then Age', () => {
    const d = crdDescriptor(crd, 'v1');
    expect(d.columns.map((c) => c.title)).toEqual([
      'Name',
      'Namespace',
      'State',
      'Replicas',
      'Since',
      'Ratio',
      'Age',
    ]);
    expect(d.group).toBe('example.com');
    expect(d.kind).toBe('Widget');
    expect(d.resource).toBe('widgets');
    expect(d.namespaced).toBe(true);
  });

  it('shows dates as ages and sorts integers and numbers numerically', () => {
    const cols = crdDescriptor(crd, 'v1').columns;
    const byTitle = (t: string) => cols.find((c) => c.title === t)!;
    expect(byTitle('State').value(widget, now)).toBe('Ready');
    expect(byTitle('Replicas').value(widget, now)).toBe(3);
    expect(byTitle('Replicas').sort).toBe('number');
    expect(byTitle('Ratio').value(widget, now)).toBe(0.5);
    expect(byTitle('Ratio').sort).toBe('number');
    expect(byTitle('Since').value(widget, now)).toBe('1h');
    expect(byTitle('Since').sort).toBe('age');
  });

  it('gives a missing numeric value 0 and a missing text value an empty string', () => {
    const cols = crdDescriptor(crd, 'v1').columns;
    expect(cols.find((c) => c.title === 'Replicas')!.value({ metadata: {} }, now)).toBe(0);
    expect(cols.find((c) => c.title === 'State')!.value({ metadata: {} }, now)).toBe('');
  });

  it('omits Namespace for a cluster-scoped kind', () => {
    const d = crdDescriptor({ spec: { ...crd.spec, scope: 'Cluster' } }, 'v1');
    expect(d.namespaced).toBe(false);
    expect(d.columns.map((c) => c.title)).toEqual(['Name', 'State', 'Replicas', 'Since', 'Ratio', 'Age']);
  });

  it('falls back to Name, Namespace and Age when the version has no columns or is unknown', () => {
    expect(crdDescriptor(crd, 'v2').columns.map((c) => c.title)).toEqual(['Name', 'Namespace', 'Age']);
    expect(crdDescriptor(crd, 'v9').columns.map((c) => c.title)).toEqual(['Name', 'Namespace', 'Age']);
  });

  it('skips a printer column that has no path', () => {
    const d = crdDescriptor(
      {
        spec: {
          ...crd.spec,
          versions: [{ name: 'v1', additionalPrinterColumns: [{ name: 'Bad', type: 'string' }] }],
        },
      },
      'v1',
    );
    expect(d.columns.map((c) => c.title)).toEqual(['Name', 'Namespace', 'Age']);
  });

  it('without a CRD the descriptor is Name, Namespace, Age', () => {
    const d = crdDescriptor(undefined, 'v1', {
      group: 'example.com',
      kind: 'Widget',
      resource: 'widgets',
      namespaced: true,
    });
    expect(d.columns.map((c) => c.title)).toEqual(['Name', 'Namespace', 'Age']);
    expect(d.title).toBe('Widget');
  });
});
