import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Legend, mapNodes, nodeStyle, type MapNode } from './legend';

const node = (over: Partial<MapNode>): MapNode => ({
  id: 'x',
  group: '',
  kind: 'Pod',
  namespace: 'ns',
  name: 'p',
  ...over,
});

describe('nodeStyle', () => {
  it('colours pods by phase, and marks a failed pod with a warning icon and a label', () => {
    const phases = ['Running', 'Pending', 'Failed', 'Succeeded', 'Unknown', ''];
    const colours = phases.map((p) => nodeStyle(node({ status: p })).color);
    expect(new Set(colours).size).toBeGreaterThanOrEqual(4);
    const failed = nodeStyle(node({ status: 'Failed' }));
    expect(failed.icon).toBe('⚠');
    expect(failed.statusText).toBe('failed');
    expect(nodeStyle(node({ status: 'Running' })).statusText).toBe('running');
    expect(nodeStyle(node({ status: 'Running' })).graphStatus).toBe('ok');
    expect(nodeStyle(node({ status: 'Pending' })).graphStatus).toBe('warning');
    expect(nodeStyle(node({ status: 'Failed' })).graphStatus).toBe('error');
  });

  it('gives every other kind its own colour and icon, and no status', () => {
    const kinds = [
      'Deployment',
      'StatefulSet',
      'DaemonSet',
      'Service',
      'Ingress',
      'HTTPRoute',
      'GRPCRoute',
      'ConfigMap',
      'Secret',
      'PersistentVolumeClaim',
      'HelmRelease',
    ];
    const styles = kinds.map((k) => nodeStyle(node({ kind: k, status: undefined })));
    expect(new Set(styles.map((s) => s.color)).size).toBe(kinds.length);
    expect(new Set(styles.map((s) => s.icon)).size).toBe(kinds.length);
    expect(styles.every((s) => s.statusText === '')).toBe(true);
  });

  it('has a fallback for a kind it does not know', () => {
    expect(nodeStyle(node({ kind: 'Mystery' })).color).toMatch(/^#/);
  });
});

describe('mapNodes', () => {
  it('turns the nodes of the server into graph nodes with their facts', () => {
    const [n] = mapNodes([node({ kind: 'Pod', status: 'Failed', namespace: 'shop', name: 'web-1' })]);
    expect(n).toMatchObject({ id: 'x', kind: 'Pod', name: 'web-1', status: 'error', statusText: 'failed' });
    expect(n?.facts).toEqual(['Namespace shop', 'Phase Failed']);
    expect(mapNodes([node({ kind: 'Service', status: undefined, namespace: '' })])[0]?.facts).toEqual([]);
  });
});

describe('Legend', () => {
  it('names every kind and the pod colours, and says failed pods are marked', () => {
    render(<Legend />);
    const legend = screen.getByRole('region', { name: 'Legend' });
    for (const kind of [
      'Deployment',
      'Service',
      'Ingress',
      'ConfigMap',
      'Secret',
      'PersistentVolumeClaim',
      'HelmRelease',
    ]) {
      expect(within(legend).getByText(kind)).toBeInTheDocument();
    }
    for (const phase of ['Running', 'Pending', 'Failed']) {
      expect(within(legend).getByText(new RegExp(`Pod .*${phase}`, 'i'))).toBeInTheDocument();
    }
    expect(within(legend).getByText(/failed pods are marked/i)).toBeInTheDocument();
  });
});
