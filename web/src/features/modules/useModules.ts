import { useQuery } from '@tanstack/react-query';
import { useApi } from '../../api/context';
import type { ModuleStatus } from './registry';

/** The modules of a cluster; undefined while they load. A failed request reads as no modules, so nothing is hidden. */
export function useModules(cluster: string, enabled = true): ModuleStatus[] | undefined {
  const api = useApi();
  const query = useQuery({
    queryKey: ['modules', cluster],
    queryFn: () => api.get<{ modules: ModuleStatus[] }>(`/clusters/${encodeURIComponent(cluster)}/modules`),
    enabled,
    staleTime: 60_000,
    retry: false,
  });
  return query.isError ? [] : query.data?.modules;
}
