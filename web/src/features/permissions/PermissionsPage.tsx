import { useCallback, useEffect, useState } from 'react';
import type { K8sObject } from '../../state/live';
import { useLive } from '../../state/useLive';
import type { ModuleKind } from '../modules/registry';
import { GatekeeperConstraints, type ConstraintRef } from './GatekeeperConstraints';
import { KyvernoPolicies, type PolicyRef } from './KyvernoPolicies';
import { ServiceAccountPermissions } from './ServiceAccountPermissions';

type Sink = (kind: string, items: K8sObject[]) => void;

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

/** ServiceAccount permissions (always), and the Kyverno and Gatekeeper sections when the cluster serves them. */
export function PermissionsPage({ cluster, kinds }: { cluster: string; kinds: ModuleKind[] }) {
  const [lists, setLists] = useState<Record<string, K8sObject[]>>({});
  const sink = useCallback<Sink>(
    (k, items) => setLists((l) => (l[k] === items ? l : { ...l, [k]: items })),
    [],
  );
  const kyverno = kinds.filter((k) => k.group === 'kyverno.io');
  const gatekeeper = kinds.filter((k) => k.group === 'constraints.gatekeeper.sh');
  const policies: PolicyRef[] = kyverno.flatMap((k) =>
    (lists[k.kind] ?? []).map((object) => ({ kind: k.kind, object })),
  );
  const reports = kinds.filter((k) => k.group === 'wgpolicyk8s.io').flatMap((k) => lists[k.kind] ?? []);
  const constraints: ConstraintRef[] = gatekeeper.flatMap((k) =>
    (lists[k.kind] ?? []).map((object) => ({ kind: k.kind, object })),
  );
  return (
    <>
      {kinds.map((k) => (
        <Probe key={`${k.group}/${k.kind}`} cluster={cluster} kind={k} sink={sink} />
      ))}
      <ServiceAccountPermissions cluster={cluster} />
      {kyverno.length > 0 && <KyvernoPolicies policies={policies} reports={reports} />}
      {gatekeeper.length > 0 && <GatekeeperConstraints constraints={constraints} />}
    </>
  );
}
