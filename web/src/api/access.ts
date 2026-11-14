import { useQuery } from '@tanstack/react-query';
import { useApi } from './context';

export interface AccessCheck {
  verb: string;
  group: string;
  resource: string;
  subresource?: string;
  namespace?: string;
  name?: string;
}

export interface AccessResult {
  allowed: boolean;
  reason?: string;
}

/** Asks what the user may do (`POST /clusters/{cluster}/access`); results come back in the order of `checks`. */
export function useAccess(cluster: string, checks: AccessCheck[], enabled = true) {
  const api = useApi();
  return useQuery({
    queryKey: ['access', cluster, checks],
    queryFn: async () =>
      (
        await api.post<{ results: AccessResult[] }>(`/clusters/${encodeURIComponent(cluster)}/access`, {
          checks,
        })
      ).results,
    enabled: enabled && checks.length > 0,
    staleTime: 60_000,
  });
}
