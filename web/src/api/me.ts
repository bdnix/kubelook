import { useQuery } from '@tanstack/react-query';
import { ApiError } from './client';
import { useApi } from './context';

/** The signed-in user. The full payload arrives with Step 2.4. */
export interface Me {
  id: string;
  email: string;
  name: string;
}

export type MeState =
  { state: 'loading' } | { state: 'signedIn'; me: Me } | { state: 'signedOut' } | { state: 'error' };

export function useMe(): MeState {
  const api = useApi();
  const query = useQuery({ queryKey: ['me'], queryFn: () => api.get<Me>('/me') });
  if (query.isPending) return { state: 'loading' };
  if (query.isError) {
    return query.error instanceof ApiError && query.error.status === 401
      ? { state: 'signedOut' }
      : { state: 'error' };
  }
  return { state: 'signedIn', me: query.data };
}
