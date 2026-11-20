import { useMemo, useState } from 'react';
import { conditionStatus } from '../../kinds/columns';
import { getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { groupKey, overlayMatches, poolUsage, type Bar } from './usage';

const POOL = 'karpenter.sh/nodepool';

const labelsOf = (o: K8sObject): Record<string, string> =>
  (o.metadata as { labels?: Record<string, string> } | undefined)?.labels ?? {};
const managed = (n: K8sObject) => POOL in labelsOf(n);
const nodeReady = (n: K8sObject) => conditionStatus('Ready')(n) === 'True';

function Usage({ label, bar, format }: { label: string; bar: Bar; format: (n: number) => string }) {
  const max = bar.limit ?? Math.max(bar.used, 1);
  return (
    <p>
      {`${label}: ${format(bar.used)}${bar.limit !== undefined ? ` of ${format(bar.limit)}` : ' (no limit)'} `}
      <progress
        role="progressbar"
        aria-label={label}
        aria-valuenow={bar.used}
        aria-valuemin={0}
        aria-valuemax={max}
        value={bar.used}
        max={max}
      />
    </p>
  );
}

const cores = (n: number) => `${Math.round(n * 100) / 100} cores`;
const gib = (n: number) => `${Math.round((n / 1024 ** 3) * 10) / 10} GiB`;

export interface OverviewProps {
  cluster: string;
  nodes: K8sObject[];
  pools: K8sObject[];
  claims: K8sObject[];
  overlays: K8sObject[];
  events: K8sObject[];
  now: number;
}

function PoolCard({ pool, nodes, overlays }: { pool: K8sObject; nodes: K8sObject[]; overlays: K8sObject[] }) {
  const name = getString(pool, 'metadata.name');
  const usage = poolUsage(pool, nodes);
  const types = [
    ...new Set(nodes.map((n) => groupKey(n, 'instanceType')).filter((t) => t !== 'none')),
  ].sort();
  const isReady = conditionStatus('Ready')(pool) === 'True';
  const applies = overlays.filter((o) => overlayMatches(o, nodes));
  const rejected = applies.filter((o) => conditionStatus('Ready')(o) === 'False');
  const accepted = applies.filter((o) => conditionStatus('Ready')(o) !== 'False');
  return (
    <section aria-label={`NodePool ${name}`}>
      <h3>{name}</h3>
      <p>{isReady ? 'ready' : 'not ready'}</p>
      <dl>
        <dt>NodeClass</dt>
        <dd>{getString(pool, 'spec.template.spec.nodeClassRef.name')}</dd>
        <dt>Nodes</dt>
        <dd>{nodes.length}</dd>
        <dt>Instance types</dt>
        <dd>{types.join(', ') || 'none'}</dd>
        <dt>Expire after</dt>
        <dd>{getString(pool, 'spec.template.spec.expireAfter') || 'never'}</dd>
      </dl>
      <Usage label="CPU" bar={usage.cpu} format={cores} />
      <Usage label="Memory" bar={usage.memory} format={gib} />
      {(accepted.length > 0 || rejected.length > 0) && (
        <>
          <h4>NodeOverlays</h4>
          <ul>
            {accepted.map((o) => (
              <li key={getString(o, 'metadata.name')}>{getString(o, 'metadata.name')}</li>
            ))}
            {rejected.map((o) => {
              const message =
                (
                  (o.status as { conditions?: { type: string; message?: string }[] } | undefined)
                    ?.conditions ?? []
                ).find((c) => c.type === 'Ready')?.message ?? '';
              return (
                <li
                  key={getString(o, 'metadata.name')}
                >{`${getString(o, 'metadata.name')} (rejected${message ? `: ${message}` : ''})`}</li>
              );
            })}
          </ul>
        </>
      )}
    </section>
  );
}

/** Totals and a card for each NodePool, with filters. */
export function KarpenterOverview({ nodes, pools, claims, overlays }: OverviewProps) {
  const [name, setName] = useState('');
  const [instanceType, setInstanceType] = useState('');
  const [poolStatus, setPoolStatus] = useState('');
  const [nodeStatus, setNodeStatus] = useState('');
  const poolNodes = useMemo(() => {
    const m = new Map<string, K8sObject[]>();
    for (const n of nodes)
      if (managed(n)) m.set(groupKey(n, 'pool'), [...(m.get(groupKey(n, 'pool')) ?? []), n]);
    return m;
  }, [nodes]);
  const managedCount = [...poolNodes.values()].reduce((s, l) => s + l.length, 0);
  const readyPools = pools.filter((p) => conditionStatus('Ready')(p) === 'True').length;
  const unbound = claims.filter((c) => getString(c, 'status.nodeName') === '').length;
  const shown = pools.filter((p) => {
    const pn = getString(p, 'metadata.name');
    const mine = poolNodes.get(pn) ?? [];
    if (name && !pn.toLowerCase().includes(name.toLowerCase())) return false;
    if (poolStatus === 'ready' && conditionStatus('Ready')(p) !== 'True') return false;
    if (poolStatus === 'notReady' && conditionStatus('Ready')(p) === 'True') return false;
    if (
      instanceType &&
      !mine.some((n) => groupKey(n, 'instanceType').toLowerCase().includes(instanceType.toLowerCase()))
    )
      return false;
    if (nodeStatus === 'ready' && !mine.some(nodeReady)) return false;
    if (nodeStatus === 'notReady' && !mine.some((n) => !nodeReady(n))) return false;
    return true;
  });
  return (
    <>
      <section aria-label="Totals">
        <ul>
          <li>
            Nodes <strong>{nodes.length}</strong>
          </li>
          <li>
            Managed by Karpenter <strong>{managedCount}</strong>
          </li>
          <li>
            Other nodes <strong>{nodes.length - managedCount}</strong>
          </li>
          <li>
            NodePools ready <strong>{`${readyPools} of ${pools.length}`}</strong>
          </li>
          <li>
            NodeClaims without a node <strong>{unbound}</strong>
          </li>
        </ul>
      </section>
      <div role="search" aria-label="Filters">
        <label>
          Filter by name
          <input type="text" value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <label>
          Filter by instance type
          <input type="text" value={instanceType} onChange={(e) => setInstanceType(e.target.value)} />
        </label>
        <label>
          Pool status
          <select value={poolStatus} onChange={(e) => setPoolStatus(e.target.value)}>
            <option value="">All</option>
            <option value="ready">Ready</option>
            <option value="notReady">Not ready</option>
          </select>
        </label>
        <label>
          Node status
          <select value={nodeStatus} onChange={(e) => setNodeStatus(e.target.value)}>
            <option value="">All</option>
            <option value="ready">Ready</option>
            <option value="notReady">Not ready</option>
          </select>
        </label>
      </div>
      {pools.length === 0 ? (
        <p>There are no NodePools.</p>
      ) : (
        shown.map((p) => (
          <PoolCard
            key={getString(p, 'metadata.name')}
            pool={p}
            nodes={poolNodes.get(getString(p, 'metadata.name')) ?? []}
            overlays={overlays}
          />
        ))
      )}
    </>
  );
}
