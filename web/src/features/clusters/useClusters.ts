import { useQuery } from '@tanstack/react-query';
import { useApi } from '../../api/context';

export type ClusterState = 'closed' | 'open' | 'disconnected';

export interface ClusterEntry {
  id: string;
  name: string;
  roles?: string[];
  selectedRole?: string;
  state: ClusterState;
  server?: string;
}

export interface Source {
  type: 'vault' | 'kubeconfig';
  label: string;
  status: 'ok' | 'unreachable' | 'expired';
  clusters: ClusterEntry[];
}

/** The clusters of the session, grouped by the credential they come from. */
export function useClusters() {
  const api = useApi();
  return useQuery({
    queryKey: ['clusters'],
    queryFn: async () => (await api.get<{ sources: Source[] }>('/clusters')).sources,
  });
}
