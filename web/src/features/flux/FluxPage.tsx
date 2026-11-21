import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useDiscovery } from '../../api/discovery';
import { Banner } from '../../components/Banner';
import type { K8sObject } from '../../state/live';
import { useLive } from '../../state/useLive';
import { FluxDashboardView } from './FluxDashboard';
import { FluxGraph } from './FluxGraph';
import { controllerHealth, servedFluxKinds, summarize, type ServedFluxKind } from './summary';

type Sink = (id: string, items: K8sObject[]) => void;

const idOf = (s: ServedFluxKind) => `${s.group}/${s.def.kind}`;

/** Watches one Flux kind in all namespaces and hands its objects, stamped with apiVersion and kind, to the page. */
function KindProbe({ cluster, served, sink }: { cluster: string; served: ServedFluxKind; sink: Sink }) {
  const live = useLive({
    cluster,
    group: served.group,
    version: served.version,
    resource: served.resource,
    namespace: '',
  });
  const stamped = useMemo(
    () =>
      live.items.map((o) => ({
        apiVersion: `${served.group}/${served.version}`,
        kind: served.def.kind,
        ...o,
      })),
    [live.items, served],
  );
  useEffect(() => {
    if (live.status === 'live' || live.status === 'paused') sink(idOf(served), stamped);
  }, [live.status, stamped, served, sink]);
  return null;
}

function DeploymentsProbe({ cluster, sink }: { cluster: string; sink: Sink }) {
  const live = useLive({ cluster, group: 'apps', version: 'v1', resource: 'deployments', namespace: '' });
  useEffect(() => {
    if (live.status === 'live' || live.status === 'paused') sink('deployments', live.items);
  }, [live.status, live.items, sink]);
  return null;
}

/** The Flux page: counts per kind, controller health and the graph, from live lists of the kinds the cluster serves. */
export function FluxPage({ cluster }: { cluster: string }): ReactNode {
  const discovery = useDiscovery(cluster);
  const served = useMemo(() => (discovery.data ? servedFluxKinds(discovery.data) : []), [discovery.data]);
  const [lists, setLists] = useState<Record<string, K8sObject[]>>({});
  const sink = useCallback<Sink>(
    (id, items) => setLists((l) => (l[id] === items ? l : { ...l, [id]: items })),
    [],
  );
  const rows = served.map((kind) => ({ kind, summary: summarize(kind.def, lists[idOf(kind)] ?? []) }));
  const objects = useMemo(() => served.flatMap((s) => lists[idOf(s)] ?? []), [served, lists]);
  if (discovery.isError) return <Banner kind="error">{discovery.error.message}</Banner>;
  if (!discovery.data) return <p role="status">Loading…</p>;
  return (
    <>
      <h1>FluxCD</h1>
      {served.map((s) => (
        <KindProbe key={idOf(s)} cluster={cluster} served={s} sink={sink} />
      ))}
      <DeploymentsProbe cluster={cluster} sink={sink} />
      <FluxDashboardView
        cluster={cluster}
        rows={rows}
        controllers={controllerHealth(lists.deployments ?? [])}
      />
      <FluxGraph cluster={cluster} objects={objects} />
    </>
  );
}
