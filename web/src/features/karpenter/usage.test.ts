import { describe, expect, it } from 'vitest';
import { overlayMatches, poolUsage, groupNodes, sizeOf } from './usage';

const node = (name: string, labels: Record<string, string>, cpu = '4', memory = '16Gi', pods = '58') => ({
  metadata: { name, labels },
  status: { capacity: { cpu, memory, pods }, allocatable: { cpu, memory, pods } },
});

describe('poolUsage', () => {
  it('sums the capacity of the nodes of the pool and compares it with the limits', () => {
    const u = poolUsage({ spec: { limits: { cpu: '100', memory: '400Gi' } } }, [
      node('a', {}),
      node('b', {}, '8', '32Gi'),
    ]);
    expect(u.cpu).toEqual({ used: 12, limit: 100 });
    expect(u.memory.used).toBe(48 * 1024 ** 3);
    expect(u.memory.limit).toBe(400 * 1024 ** 3);
  });

  it('has no limit when the pool sets none', () => {
    expect(poolUsage({ spec: {} }, [node('a', {})]).cpu.limit).toBeUndefined();
  });

  it('is zero without nodes', () => {
    expect(poolUsage({ spec: { limits: { cpu: '10' } } }, []).cpu).toEqual({ used: 0, limit: 10 });
  });
});

describe('overlayMatches', () => {
  const nodes = [
    node('a', { 'node.kubernetes.io/instance-type': 'm5.large', 'karpenter.sh/capacity-type': 'spot' }),
  ];
  const overlay = (requirements: unknown[]) => ({ spec: { requirements } });
  it.each([
    [[{ key: 'node.kubernetes.io/instance-type', operator: 'In', values: ['m5.large', 'm5.xlarge'] }], true],
    [[{ key: 'node.kubernetes.io/instance-type', operator: 'In', values: ['c5.large'] }], false],
    [[{ key: 'karpenter.sh/capacity-type', operator: 'NotIn', values: ['on-demand'] }], true],
    [[{ key: 'karpenter.sh/capacity-type', operator: 'NotIn', values: ['spot'] }], false],
    [[{ key: 'karpenter.sh/capacity-type', operator: 'Exists' }], true],
    [[{ key: 'missing', operator: 'Exists' }], false],
    [[{ key: 'missing', operator: 'DoesNotExist' }], true],
    [[], true],
  ])('%j → %s', (requirements, want) => {
    expect(overlayMatches(overlay(requirements), nodes)).toBe(want);
  });
});

describe('groupNodes and sizeOf', () => {
  const nodes = [
    node('a', {
      'karpenter.sh/nodepool': 'p1',
      'topology.kubernetes.io/zone': 'z1',
      'node.kubernetes.io/instance-type': 'm5.large',
    }),
    node('b', {
      'karpenter.sh/nodepool': 'p1',
      'topology.kubernetes.io/zone': 'z2',
      'node.kubernetes.io/instance-type': 'm5.large',
    }),
    node('c', {
      'karpenter.sh/nodepool': 'p2',
      'topology.kubernetes.io/zone': 'z1',
      'node.kubernetes.io/instance-type': 'c5.large',
    }),
  ];
  it('groups by pool, zone, instance type or node class', () => {
    expect([...groupNodes(nodes, 'pool').keys()]).toEqual(['p1', 'p2']);
    expect(groupNodes(nodes, 'zone').get('z1')?.length).toBe(2);
    expect(groupNodes(nodes, 'instanceType').get('m5.large')?.length).toBe(2);
    expect(groupNodes([node('x', {})], 'pool').get('none')?.length).toBe(1);
  });
  it('sizes by cpu, memory, pods or equally', () => {
    expect(sizeOf(nodes[0] as never, 'cpu')).toBe(4);
    expect(sizeOf(nodes[0] as never, 'memory')).toBe(16 * 1024 ** 3);
    expect(sizeOf(nodes[0] as never, 'pods')).toBe(58);
    expect(sizeOf(nodes[0] as never, 'equal')).toBe(1);
  });
});
