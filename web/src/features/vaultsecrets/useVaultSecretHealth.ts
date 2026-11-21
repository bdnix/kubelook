import { findResource, useDiscovery } from '../../api/discovery';
import { conditionStatus } from '../../kinds/columns';
import { useLive } from '../../state/useLive';

/** How many VaultSecrets could not create their Secret (`SecretCreated=False`); undefined while unknown. */
export function useVaultSecretHealth(
  cluster: string,
  enabled = true,
): { failed: number; total: number } | undefined {
  const discovery = useDiscovery(cluster, enabled);
  const resource = discovery.data ? findResource(discovery.data, 'ricoberger.de', 'VaultSecret') : undefined;
  const live = useLive(
    enabled && resource
      ? {
          cluster,
          group: 'ricoberger.de',
          version: resource.version,
          resource: resource.resource,
          namespace: '',
        }
      : undefined,
  );
  if (!enabled || !resource || live.status === 'loading') return undefined;
  return {
    failed: live.items.filter((o) => conditionStatus('SecretCreated')(o) === 'False').length,
    total: live.items.length,
  };
}
