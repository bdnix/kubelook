import { useState } from 'react';
import { findResource, useDiscovery } from '../../api/discovery';
import { useNow } from '../../lib/clock';
import { getString } from '../../lib/path';
import { useLive } from '../../state/useLive';
import { KarpenterOverview } from './Overview';
import { ScalingDecisions } from './ScalingDecisions';
import { Topology } from './Topology';

const MINUTE = 60_000;
type Tab = 'Overview' | 'Topology' | 'Scaling decisions';
const TABS: Tab[] = ['Overview', 'Topology', 'Scaling decisions'];
const KINDS = new Set(['NodeClaim', 'NodePool', 'NodeOverlay']);

/** The Karpenter module: overview, topology and scaling decisions, from live lists. */
export function KarpenterPage({ cluster }: { cluster: string }) {
  const discovery = useDiscovery(cluster);
  const [tab, setTab] = useState<Tab>('Overview');
  const now = useNow(MINUTE);
  const overlay = discovery.data ? findResource(discovery.data, 'karpenter.sh', 'NodeOverlay') : undefined;
  const nodes = useLive({ cluster, group: '', version: 'v1', resource: 'nodes', namespace: '' });
  const pools = useLive({
    cluster,
    group: 'karpenter.sh',
    version: 'v1',
    resource: 'nodepools',
    namespace: '',
  });
  const claims = useLive({
    cluster,
    group: 'karpenter.sh',
    version: 'v1',
    resource: 'nodeclaims',
    namespace: '',
  });
  const overlays = useLive(
    overlay
      ? { cluster, group: 'karpenter.sh', version: overlay.version, resource: 'nodeoverlays', namespace: '' }
      : undefined,
  );
  const events = useLive(
    tab === 'Scaling decisions'
      ? { cluster, group: 'events.k8s.io', version: 'v1', resource: 'events', namespace: '' }
      : undefined,
  );
  const karpenterEvents = events.items.filter((e) => {
    const ref = (e.regarding ?? e.involvedObject) as { kind?: string } | undefined;
    return KINDS.has(ref?.kind ?? '') || getString(e, 'reportingController').includes('karpenter');
  });
  return (
    <>
      <div role="tablist" aria-label="Karpenter">
        {TABS.map((t) => (
          <button key={t} type="button" role="tab" aria-selected={tab === t} onClick={() => setTab(t)}>
            {t}
          </button>
        ))}
      </div>
      {tab === 'Overview' && (
        <KarpenterOverview
          cluster={cluster}
          nodes={nodes.items}
          pools={pools.items}
          claims={claims.items}
          overlays={overlays.items}
          events={[]}
          now={now}
        />
      )}
      {tab === 'Topology' && <Topology nodes={nodes.items} />}
      {tab === 'Scaling decisions' && <ScalingDecisions events={karpenterEvents} now={now} />}
    </>
  );
}
