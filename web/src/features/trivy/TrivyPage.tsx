import { useCallback, useEffect, useState } from 'react';
import type { K8sObject } from '../../state/live';
import { useLive } from '../../state/useLive';
import type { ModuleKind } from '../modules/registry';
import { TrivyOverviewView, type ReportRef } from './TrivyOverview';

type Sink = (id: string, reports: ReportRef[]) => void;

function Probe({ cluster, kind, sink }: { cluster: string; kind: ModuleKind; sink: Sink }) {
  const live = useLive({
    cluster,
    group: kind.group,
    version: kind.version,
    resource: kind.resource,
    namespace: '',
  });
  useEffect(() => {
    if (live.status === 'live' || live.status === 'paused')
      sink(
        kind.kind,
        live.items.map((object: K8sObject) => ({ kind: kind.kind, group: kind.group, object })),
      );
  }, [live.status, live.items, kind, sink]);
  return null;
}

/** The Trivy overview: totals and the worst reports of every report kind the cluster serves. */
export function TrivyPage({ cluster, kinds }: { cluster: string; kinds: ModuleKind[] }) {
  const [lists, setLists] = useState<Record<string, ReportRef[]>>({});
  const sink = useCallback<Sink>(
    (id, reports) => setLists((l) => (l[id] === reports ? l : { ...l, [id]: reports })),
    [],
  );
  return (
    <>
      {kinds.map((k) => (
        <Probe key={k.kind} cluster={cluster} kind={k} sink={sink} />
      ))}
      <TrivyOverviewView cluster={cluster} reports={Object.values(lists).flat()} />
    </>
  );
}
