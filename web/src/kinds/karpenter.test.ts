import { describe, expect, it } from 'vitest';
import { karpenterDescriptors } from './karpenter';

const find = (kind: string) => karpenterDescriptors().find((d) => d.kind === kind);
const titles = (kind: string) => find(kind)?.columns.map((c) => c.title);
const value = (kind: string, id: string, o: unknown) => find(kind)?.columns.find((c) => c.id === id)?.value(o as never, 0);

describe('karpenterDescriptors', () => {
  it('has the columns of kubectl get -o wide', () => {
    expect(titles('NodePool')).toEqual(['Name', 'NodeClass', 'Nodes', 'Ready', 'Age', 'Weight', 'CPU', 'Memory']);
    expect(titles('NodeClaim')).toEqual(['Name', 'Type', 'Capacity', 'Zone', 'Node', 'Ready', 'Age', 'ID', 'NodePool', 'NodeClass', 'Drifted']);
    expect(titles('NodeOverlay')).toEqual(['Name', 'Ready', 'Weight', 'Age']);
    for (const k of ['EC2NodeClass', 'AKSNodeClass', 'GCENodeClass']) expect(titles(k)).toEqual(['Name', 'Ready', 'Age']);
  });

  it('reads the pool columns', () => {
    const pool = {
      metadata: { name: 'default', creationTimestamp: '2026-11-20T10:00:00Z' },
      spec: { weight: 10, template: { spec: { nodeClassRef: { name: 'ec2' } } } },
      status: { nodes: 3, resources: { cpu: '12', memory: '48Gi' }, conditions: [{ type: 'Ready', status: 'True' }] },
    };
    expect(value('NodePool', 'nodeclass', pool)).toBe('ec2');
    expect(value('NodePool', 'nodes', pool)).toBe(3);
    expect(value('NodePool', 'ready', pool)).toBe('True');
    expect(value('NodePool', 'weight', pool)).toBe(10);
    expect(value('NodePool', 'cpu', pool)).toBe('12');
    expect(value('NodePool', 'memory', pool)).toBe('48Gi');
  });

  it('reads the claim columns from labels, spec and status', () => {
    const claim = {
      metadata: { name: 'c1', labels: { 'node.kubernetes.io/instance-type': 'm5.large', 'karpenter.sh/capacity-type': 'spot', 'topology.kubernetes.io/zone': 'z1', 'karpenter.sh/nodepool': 'default' } },
      spec: { nodeClassRef: { name: 'ec2' } },
      status: { nodeName: 'ip-1', providerID: 'aws:///z1/i-1', conditions: [{ type: 'Ready', status: 'True' }, { type: 'Drifted', status: 'True' }], capacity: { cpu: '2', memory: '8Gi' } },
    };
    expect(value('NodeClaim', 'type', claim)).toBe('m5.large');
    expect(value('NodeClaim', 'capacity', claim)).toBe('spot');
    expect(value('NodeClaim', 'zone', claim)).toBe('z1');
    expect(value('NodeClaim', 'node', claim)).toBe('ip-1');
    expect(value('NodeClaim', 'id', claim)).toBe('aws:///z1/i-1');
    expect(value('NodeClaim', 'nodepool', claim)).toBe('default');
    expect(value('NodeClaim', 'nodeclass', claim)).toBe('ec2');
    expect(value('NodeClaim', 'drifted', claim)).toBe('True');
  });

  it('are cluster scoped and belong to the Modules section', () => {
    for (const d of karpenterDescriptors()) {
      expect(d.namespaced).toBe(false);
      expect(d.section).toBe('Modules');
    }
  });

  it('shows the status of a pool as a badge', () => {
    const d = find('NodePool');
    expect(d?.status?.({ status: { conditions: [{ type: 'Ready', status: 'False' }] } }).severity).toBe('error');
    expect(d?.status?.({ status: { conditions: [{ type: 'Ready', status: 'True' }] } }).severity).toBe('ok');
  });
});
