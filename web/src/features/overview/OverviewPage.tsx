import { useAccess } from '../../api/access';
import { findResource, useDiscovery } from '../../api/discovery';
import { conditionStatus } from '../../kinds/columns';
import { useNow } from '../../lib/clock';
import { useLive } from '../../state/useLive';
import { useReleases } from '../helm/api';
import type { ModuleKind } from '../modules/registry';
import { useModules } from '../modules/useModules';
import { useVaultSecretHealth } from '../vaultsecrets/useVaultSecretHealth';
import { OverviewView } from './OverviewView';
import { failedFlux, failedHelm } from './triage';

const MINUTE = 60_000;

const key = (cluster: string, k: ModuleKind | undefined) =>
  k ? { cluster, group: k.group, version: k.version, resource: k.resource, namespace: '' } : undefined;

/**
 * The landing page of a cluster. It watches pods, nodes and events in all namespaces and, for each active
 * module, only what its failure count needs: at most eight watches in all, all closed when the page is left.
 */
export function OverviewPage({ cluster }: { cluster: string }) {
  const now = useNow(MINUTE);
  const discovery = useDiscovery(cluster);
  const modules = useModules(cluster);
  const access = useAccess(cluster, [
    { verb: 'list', group: '', resource: 'pods' },
    { verb: 'list', group: '', resource: 'nodes' },
    { verb: 'list', group: 'events.k8s.io', resource: 'events' },
  ]);
  const allowed = (i: number) => access.data?.[i]?.allowed !== false;
  const kindsOf = (name: string) => modules?.find((m) => m.name === name && m.active)?.kinds;
  const flux = kindsOf('flux');
  const karpenter = kindsOf('karpenter');
  const find = (kinds: ModuleKind[] | undefined, kind: string) => kinds?.find((k) => k.kind === kind);

  const pods = useLive(
    allowed(0) ? { cluster, group: '', version: 'v1', resource: 'pods', namespace: '' } : undefined,
  );
  const nodes = useLive(
    allowed(1) ? { cluster, group: '', version: 'v1', resource: 'nodes', namespace: '' } : undefined,
  );
  const events = useLive(
    allowed(2)
      ? { cluster, group: 'events.k8s.io', version: 'v1', resource: 'events', namespace: '' }
      : undefined,
  );
  const kustomizations = useLive(key(cluster, find(flux, 'Kustomization')));
  const helmReleases = useLive(key(cluster, find(flux, 'HelmRelease')));
  const pools = useLive(key(cluster, find(karpenter, 'NodePool')));
  const claims = useLive(key(cluster, find(karpenter, 'NodeClaim')));
  const vault = useVaultSecretHealth(cluster, kindsOf('vaultSecrets') !== undefined);
  const helm = useReleases(cluster);

  const ready = (live: { status: string }) => live.status === 'live' || live.status === 'paused';
  return (
    <OverviewView
      cluster={cluster}
      now={now}
      pods={allowed(0) && ready(pods) ? pods.items : undefined}
      nodes={allowed(1) && ready(nodes) ? nodes.items : undefined}
      events={allowed(2) && ready(events) ? events.items : undefined}
      flux={flux ? { failed: failedFlux([...kustomizations.items, ...helmReleases.items]) } : undefined}
      helm={helm.data ? { failed: failedHelm(helm.data) } : undefined}
      vaultSecrets={vault}
      karpenter={
        karpenter && discovery.data && findResource(discovery.data, 'karpenter.sh', 'NodePool')
          ? {
              pools: pools.items.length,
              ready: pools.items.filter((p) => conditionStatus('Ready')(p) === 'True').length,
              unbound: claims.items.filter(
                (c) => ((c.status as { nodeName?: string } | undefined)?.nodeName ?? '') === '',
              ).length,
            }
          : undefined
      }
    />
  );
}
