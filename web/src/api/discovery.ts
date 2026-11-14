import { useQuery } from '@tanstack/react-query';
import { useApi } from './context';

export interface DiscoveryResource {
  group: string;
  version: string;
  resource: string;
  kind: string;
  namespaced: boolean;
  verbs: string[];
  preferred: boolean;
  servedVersions: string[];
}

/** What a cluster serves (`GET /clusters/{cluster}/discovery`). */
export interface Discovery {
  serverVersion: string;
  resources: DiscoveryResource[];
}

/** The preferred served version of a kind; `core` and the empty string both mean the core group. */
export function findResource(d: Discovery, group: string, kind: string): DiscoveryResource | undefined {
  const g = group === 'core' ? '' : group;
  return d.resources.find((r) => r.preferred && r.group === g && r.kind === kind);
}

export function useDiscovery(cluster: string, enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: ['discovery', cluster],
    queryFn: () => api.get<Discovery>(`/clusters/${encodeURIComponent(cluster)}/discovery`),
    enabled,
    staleTime: 60_000,
  });
}
