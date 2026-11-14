import { useQuery } from '@tanstack/react-query';
import { useApi } from './context';

/** What `GET /api/v1/status` says about the services the server depends on. */
export interface ServerStatus {
  vault: { configured: boolean; reachable: boolean; since: string | null };
}

export const STATUS_POLL_MS = 15_000;

/** The server's status, polled every 15 s. A failed poll keeps the last answer. */
export function useStatus() {
  const api = useApi();
  return useQuery({
    queryKey: ['status'],
    queryFn: () => api.get<ServerStatus>('/status'),
    refetchInterval: STATUS_POLL_MS,
    retry: false,
  });
}
