import { useMemo, useState } from 'react';
import { getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { treemap } from './treemap';
import { groupNodes, sizeOf, type GroupBy, type SizeBy } from './usage';

const WIDTH = 720;
const HEIGHT = 360;

const GROUPS: [GroupBy, string][] = [
  ['pool', 'NodePool'],
  ['zone', 'Zone'],
  ['instanceType', 'Instance type'],
  ['nodeClass', 'Node class'],
];
const SIZES: [SizeBy, string][] = [
  ['cpu', 'CPU'],
  ['memory', 'Memory'],
  ['pods', 'Pods'],
  ['equal', 'Equal'],
];

/** The Karpenter nodes as a treemap: grouped by pool, zone, instance type or node class, sized by CPU, memory, pods or equally. */
export function Topology({ nodes }: { nodes: K8sObject[] }) {
  const [by, setBy] = useState<GroupBy>('pool');
  const [size, setSize] = useState<SizeBy>('cpu');
  const karpenter = useMemo(
    () =>
      nodes.filter(
        (n) => 'karpenter.sh/nodepool' in ((n.metadata as { labels?: object } | undefined)?.labels ?? {}),
      ),
    [nodes],
  );
  const layout = useMemo(() => {
    const groups = groupNodes(karpenter, by);
    const outer = treemap(
      [...groups.entries()].map(([id, list]) => ({
        id,
        value: list.reduce((s, n) => s + sizeOf(n, size), 0),
      })),
      WIDTH,
      HEIGHT,
    );
    return outer.map((g) => ({
      group: g,
      cells: treemap(
        (groups.get(g.id) ?? []).map((n) => ({ id: getString(n, 'metadata.name'), value: sizeOf(n, size) })),
        g.w,
        g.h,
      ),
    }));
  }, [karpenter, by, size]);
  if (karpenter.length === 0) return <p>There are no nodes to show.</p>;
  return (
    <>
      <label>
        Group by
        <select value={by} onChange={(e) => setBy(e.target.value as GroupBy)}>
          {GROUPS.map(([v, l]) => (
            <option key={v} value={v}>
              {l}
            </option>
          ))}
        </select>
      </label>
      <label>
        Size by
        <select value={size} onChange={(e) => setSize(e.target.value as SizeBy)}>
          {SIZES.map(([v, l]) => (
            <option key={v} value={v}>
              {l}
            </option>
          ))}
        </select>
      </label>
      <svg role="img" aria-label="Node topology" viewBox={`0 0 ${WIDTH} ${HEIGHT}`} width="100%">
        {layout.map(({ group, cells }) => (
          <g key={group.id} transform={`translate(${group.x} ${group.y})`}>
            {cells.map((c) => (
              <rect
                key={c.id}
                data-node={c.id}
                x={c.x}
                y={c.y}
                width={c.w}
                height={c.h}
                fill="currentColor"
                fillOpacity={0.15}
                stroke="currentColor"
              >
                <title>{c.id}</title>
              </rect>
            ))}
            <text x={4} y={14} fontSize={12}>
              {group.id}
            </text>
          </g>
        ))}
      </svg>
    </>
  );
}
