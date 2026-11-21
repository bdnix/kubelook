import { Link } from 'react-router';
import { clusterPath } from '../../lib/paths';

export interface FailureCountsProps {
  cluster: string;
  flux?: { failed: number };
  helm?: { failed: number };
  vaultSecrets?: { failed: number; total: number };
  karpenter?: { pools: number; ready: number; unbound: number };
}

/** Counts of what failed in the ecosystem modules that are there, each linking to its page. */
export function FailureCounts({ cluster, flux, helm, vaultSecrets, karpenter }: FailureCountsProps) {
  const base = clusterPath(cluster);
  return (
    <section aria-label="Failures">
      <h2>Failures</h2>
      <ul>
        {flux && (
          <li>
            <Link to={`${base}/flux`}>{`${flux.failed} failing Flux objects`}</Link>
          </li>
        )}
        {helm && (
          <li>
            <Link
              to={`${base}/helm`}
            >{`${helm.failed} failed Helm release${helm.failed === 1 ? '' : 's'}`}</Link>
          </li>
        )}
        {vaultSecrets && (
          <li>
            <Link
              to={`${base}/ricoberger.de/VaultSecret`}
            >{`${vaultSecrets.failed} of ${vaultSecrets.total} VaultSecrets failed`}</Link>
          </li>
        )}
        {karpenter && (
          <li>
            <Link
              to={`${base}/modules/karpenter`}
            >{`Karpenter: ${karpenter.ready} of ${karpenter.pools} NodePools ready, ${karpenter.unbound} NodeClaims without a node`}</Link>
          </li>
        )}
      </ul>
    </section>
  );
}
