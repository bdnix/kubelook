import { Link } from 'react-router';
import { getArray, getString } from '../../lib/path';
import { resourcePath } from '../../lib/paths';
import type { K8sObject } from '../../state/live';

const labelOf = (o: K8sObject, key: string) =>
  ((o.metadata as { labels?: Record<string, string> } | undefined)?.labels ?? {})[key] ?? '';

/** The Karpenter section of a Node it manages: its pool and claim, instance and capacity type, zone and disruption state. */
export function NodeKarpenterSection({ cluster, node }: { cluster: string; node: K8sObject }) {
  const pool = labelOf(node, 'karpenter.sh/nodepool');
  if (pool === '') return null;
  const claim = (
    (node.metadata as { ownerReferences?: { kind?: string; name?: string }[] } | undefined)
      ?.ownerReferences ?? []
  ).find((r) => r.kind === 'NodeClaim');
  const annotations =
    (node.metadata as { annotations?: Record<string, string> } | undefined)?.annotations ?? {};
  const disrupted = getArray(node, 'spec.taints').some(
    (t) => getString(t, 'key') === 'karpenter.sh/disrupted',
  );
  return (
    <section aria-label="Karpenter">
      <h3>Karpenter</h3>
      <dl>
        <dt>NodePool</dt>
        <dd>
          <Link to={resourcePath({ cluster, group: 'karpenter.sh', kind: 'NodePool', name: pool })}>
            {pool}
          </Link>
        </dd>
        {claim?.name && (
          <>
            <dt>NodeClaim</dt>
            <dd>
              <Link
                to={resourcePath({ cluster, group: 'karpenter.sh', kind: 'NodeClaim', name: claim.name })}
              >
                {claim.name}
              </Link>
            </dd>
          </>
        )}
        <dt>Instance type</dt>
        <dd>{labelOf(node, 'node.kubernetes.io/instance-type')}</dd>
        <dt>Capacity type</dt>
        <dd>{labelOf(node, 'karpenter.sh/capacity-type')}</dd>
        <dt>Zone</dt>
        <dd>{labelOf(node, 'topology.kubernetes.io/zone')}</dd>
      </dl>
      {annotations['karpenter.sh/do-not-disrupt'] === 'true' && (
        <p>This node has the annotation karpenter.sh/do-not-disrupt, so Karpenter will not disrupt it.</p>
      )}
      {disrupted && <p>This node is being disrupted: it has the taint karpenter.sh/disrupted.</p>}
    </section>
  );
}
