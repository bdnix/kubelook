import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNow } from '../../lib/clock';
import { getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { useLive } from '../../state/useLive';
import type { ModuleKind } from '../modules/registry';
import { GatewayOverview } from './GatewayOverview';

type Sink = (kind: string, items: K8sObject[]) => void;

/** Watches one kind in all namespaces and hands its objects to the page. */
function Probe({ cluster, kind, sink }: { cluster: string; kind: ModuleKind; sink: Sink }) {
  const live = useLive({
    cluster,
    group: kind.group,
    version: kind.version,
    resource: kind.resource,
    namespace: '',
  });
  useEffect(() => {
    if (live.status === 'live' || live.status === 'paused') sink(kind.kind, live.items);
  }, [live.status, live.items, kind.kind, sink]);
  return null;
}

const MINUTE = 60_000;

/** The Gateway API overview: counts per kind and state, and the events of those objects. */
export function GatewayPage({ cluster, kinds }: { cluster: string; kinds: ModuleKind[] }) {
  const [lists, setLists] = useState<Record<string, K8sObject[]>>({});
  const sink = useCallback<Sink>(
    (k, items) => setLists((l) => (l[k] === items ? l : { ...l, [k]: items })),
    [],
  );
  const now = useNow(MINUTE);
  const events = useLive({
    cluster,
    group: 'events.k8s.io',
    version: 'v1',
    resource: 'events',
    namespace: '',
  });
  const names = useMemo(() => new Set(kinds.map((k) => k.kind)), [kinds]);
  const ours = events.items.filter((e) =>
    names.has(getString(e, 'regarding.kind') || getString(e, 'involvedObject.kind')),
  );
  return (
    <>
      {kinds.map((k) => (
        <Probe key={`${k.group}/${k.kind}`} cluster={cluster} kind={k} sink={sink} />
      ))}
      <GatewayOverview
        kinds={kinds.map((k) => ({ kind: k.kind, objects: lists[k.kind] ?? [] }))}
        events={ours}
        now={now}
      />
    </>
  );
}
