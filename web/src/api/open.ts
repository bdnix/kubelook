import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useClusters } from '../features/clusters/useClusters';
import { ApiError } from './client';
import { useApi } from './context';

export interface OpenState {
  state: 'open' | 'closed' | 'disconnected';
  server?: string;
  role?: string;
  credentialExpiresAt?: string;
}

export type OpenedCluster =
  { status: 'opening' } | { status: 'open' } | { status: 'error'; error: ApiError | Error };

/** Opens the cluster for this session when it is not open yet (`POST /clusters/{cluster}/open`). */
export function useOpenCluster(cluster: string): OpenedCluster {
  const api = useApi();
  const queryClient = useQueryClient();
  const clusters = useClusters();
  const entry = clusters.data?.flatMap((s) => s.clusters).find((c) => c.id === cluster);
  const isOpen = entry?.state === 'open';
  const open = useQuery({
    queryKey: ['open', cluster],
    queryFn: async () => {
      const state = await api.post<OpenState>(`/clusters/${encodeURIComponent(cluster)}/open`, {});
      await queryClient.invalidateQueries({ queryKey: ['clusters'] });
      return state;
    },
    enabled: clusters.isSuccess && !isOpen,
    retry: false,
    staleTime: Infinity,
    gcTime: 0,
  });
  if (isOpen) return { status: 'open' };
  if (open.isError) return { status: 'error', error: open.error };
  if (open.isSuccess && open.data.state === 'open') return { status: 'open' };
  return { status: 'opening' };
}
