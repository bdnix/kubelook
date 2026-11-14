import { describe, expect, it } from 'vitest';
import { expectToleratesMissingFields, meta, row } from '../testing';
import {
  configMapsKind,
  hpaKind,
  limitRangesKind,
  pdbKind,
  priorityClassesKind,
  resourceQuotasKind,
  secretsKind,
} from './config';

describe('config kinds', () => {
  it('ConfigMap counts data and binaryData keys', () => {
    const cm = { metadata: meta('c', 'ns'), data: { a: '1', b: '2' }, binaryData: { c: 'x' } };
    expect(row(configMapsKind, cm).keys).toBe(3);
    expectToleratesMissingFields(configMapsKind);
  });

  it('Secret shows the type and the number of keys, never values', () => {
    const s = {
      metadata: meta('s', 'ns'),
      type: 'kubernetes.io/tls',
      data: { 'tls.crt': '********', 'tls.key': '********' },
    };
    expect(row(secretsKind, s)).toMatchObject({ type: 'kubernetes.io/tls', keys: 2 });
    expectToleratesMissingFields(secretsKind);
  });

  it('ResourceQuota summarises used/hard sorted by resource', () => {
    const q = { metadata: meta('q', 'ns'), status: { hard: { pods: '10', cpu: '4' }, used: { pods: '3' } } };
    expect(row(resourceQuotasKind, q).usage).toBe('cpu: 0/4, pods: 3/10');
    expect(row(resourceQuotasKind, { metadata: meta('q', 'ns') }).usage).toBe('');
    expectToleratesMissingFields(resourceQuotasKind);
  });

  it('LimitRange has only the common columns', () => {
    expect(limitRangesKind.columns.map((c) => c.id)).toEqual(['name', 'namespace', 'age']);
  });

  it('HPA shows reference, bounds, replicas and the scaling status', () => {
    const hpa = (status: string | undefined) => ({
      metadata: meta('h', 'ns'),
      spec: { scaleTargetRef: { kind: 'Deployment', name: 'web' }, minReplicas: 2, maxReplicas: 8 },
      status: { currentReplicas: 3, conditions: status ? [{ type: 'ScalingActive', status }] : [] },
    });
    expect(row(hpaKind, hpa('True'))).toMatchObject({
      reference: 'Deployment/web',
      min: '2',
      max: 8,
      replicas: 3,
      status: 'Active',
    });
    expect(row(hpaKind, hpa('False')).status).toBe('Inactive');
    expect(row(hpaKind, hpa(undefined)).status).toBe('Unknown');
    expect(row(hpaKind, { metadata: meta('h', 'ns') })).toMatchObject({ min: '1', reference: '' });
    expectToleratesMissingFields(hpaKind);
  });

  it('PodDisruptionBudget shows numbers and percentages', () => {
    const pdb = {
      metadata: meta('p', 'ns'),
      spec: { minAvailable: '50%' },
      status: { disruptionsAllowed: 1 },
    };
    expect(row(pdbKind, pdb)).toMatchObject({ minAvailable: '50%', maxUnavailable: '', allowed: 1 });
    expect(row(pdbKind, { metadata: meta('p', 'ns'), spec: { maxUnavailable: 2 } }).maxUnavailable).toBe('2');
    expectToleratesMissingFields(pdbKind);
  });

  it('PriorityClass shows value and global default', () => {
    expect(row(priorityClassesKind, { metadata: meta('p'), value: 1000, globalDefault: true })).toMatchObject(
      { value: 1000, globalDefault: 'true' },
    );
    expect(row(priorityClassesKind, { metadata: meta('p'), value: 5 }).globalDefault).toBe('false');
    expectToleratesMissingFields(priorityClassesKind);
  });
});
