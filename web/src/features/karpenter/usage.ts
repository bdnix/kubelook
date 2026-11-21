import { parseQuantity } from '../../lib/quantity';
import { getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';

export interface Bar {
  used: number;
  limit?: number;
}

export interface PoolUsage {
  cpu: Bar;
  memory: Bar;
}

const quantity = (o: K8sObject, path: string): number => {
  const v = path
    .split('.')
    .reduce<unknown>(
      (cur, k) => (cur !== null && typeof cur === 'object' ? (cur as Record<string, unknown>)[k] : undefined),
      o,
    );
  const n = typeof v === 'string' || typeof v === 'number' ? parseQuantity(String(v)) : NaN;
  return Number.isFinite(n) ? n : 0;
};

const limit = (pool: K8sObject, key: string): number | undefined => {
  const raw = (pool.spec as { limits?: Record<string, unknown> } | undefined)?.limits?.[key];
  if (typeof raw !== 'string' && typeof raw !== 'number') return undefined;
  const n = parseQuantity(String(raw));
  return Number.isFinite(n) ? n : undefined;
};

/** The CPU and memory the nodes of a pool hold, next to the pool's `spec.limits`. */
export function poolUsage(pool: K8sObject, nodes: K8sObject[]): PoolUsage {
  const sum = (path: string) => nodes.reduce((s, n) => s + quantity(n, path), 0);
  return {
    cpu: { used: sum('status.capacity.cpu'), limit: limit(pool, 'cpu') },
    memory: { used: sum('status.capacity.memory'), limit: limit(pool, 'memory') },
  };
}

interface Requirement {
  key: string;
  operator: string;
  values?: string[];
}

function holds(r: Requirement, labels: Record<string, string>): boolean {
  const v = labels[r.key];
  switch (r.operator) {
    case 'In':
      return v !== undefined && (r.values ?? []).includes(v);
    case 'NotIn':
      return v === undefined || !(r.values ?? []).includes(v);
    case 'Exists':
      return v !== undefined;
    case 'DoesNotExist':
      return v === undefined;
    default:
      return false;
  }
}

const labelsOf = (n: K8sObject): Record<string, string> =>
  (n.metadata as { labels?: Record<string, string> } | undefined)?.labels ?? {};

/** Whether a NodeOverlay applies to the pool: some node of the pool meets every requirement of the overlay. */
export function overlayMatches(overlay: K8sObject, poolNodes: K8sObject[]): boolean {
  const reqs = (overlay.spec as { requirements?: Requirement[] } | undefined)?.requirements ?? [];
  if (reqs.length === 0) return true;
  return poolNodes.some((n) => reqs.every((r) => holds(r, labelsOf(n))));
}

export type GroupBy = 'pool' | 'zone' | 'instanceType' | 'nodeClass';
export type SizeBy = 'cpu' | 'memory' | 'pods' | 'equal';

const KEYS: Record<Exclude<GroupBy, 'nodeClass'>, string> = {
  pool: 'karpenter.sh/nodepool',
  zone: 'topology.kubernetes.io/zone',
  instanceType: 'node.kubernetes.io/instance-type',
};

export function groupKey(n: K8sObject, by: GroupBy): string {
  const labels = labelsOf(n);
  if (by === 'nodeClass') {
    const key = Object.keys(labels)
      .sort()
      .find((k) => k.endsWith('nodeclass'));
    return (key && labels[key]) || 'none';
  }
  return labels[KEYS[by]] ?? 'none';
}

/** The nodes grouped by pool, zone, instance type or node class, in sorted group order. */
export function groupNodes(nodes: K8sObject[], by: GroupBy): Map<string, K8sObject[]> {
  const out = new Map<string, K8sObject[]>();
  for (const n of nodes) {
    const k = groupKey(n, by);
    out.set(k, [...(out.get(k) ?? []), n]);
  }
  return new Map([...out.entries()].sort(([a], [b]) => a.localeCompare(b)));
}

export function sizeOf(n: K8sObject, by: SizeBy): number {
  if (by === 'equal') return 1;
  return quantity(n, `status.capacity.${by}`);
}

export const nodeName = (n: K8sObject): string => getString(n, 'metadata.name');
