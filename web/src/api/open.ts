import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useContext } from 'react';
import { useClusters } from '../features/clusters/useClusters';
import { VaultStoreContext } from '../features/credentials/context';
import { syncPending } from '../features/credentials/pending';
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
export function useOpenCluster(cluster: string, enabled = true): OpenedCluster {
  const api = useApi();
  const queryClient = useQueryClient();
  const clusters = useClusters();
  const store = useContext(VaultStoreContext);
  const entry = clusters.data?.flatMap((s) => s.clusters).find((c) => c.id === cluster);
  const isOpen = entry?.state === 'open';
  const open = useQuery({
    queryKey: ['open', cluster],
    queryFn: async () => {
      const state = await api.post<OpenState>(`/clusters/${encodeURIComponent(cluster)}/open`, {});
      await queryClient.invalidateQueries({ queryKey: ['clusters'] });
      // Opening can rotate a refresh token; the re-sealed copy has to reach this browser.
      if (store) void syncPending(api, store);
      return state;
    },
    enabled: enabled && clusters.isSuccess && !isOpen,
    retry: false,
    staleTime: Infinity,
    gcTime: 0,
  });
  if (isOpen) return { status: 'open' };
  if (open.isError) return { status: 'error', error: open.error };
  if (open.isSuccess && open.data.state === 'open') return { status: 'open' };
  return { status: 'opening' };
}
