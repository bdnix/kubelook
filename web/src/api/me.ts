import { useQuery } from '@tanstack/react-query';
import { csrfStore } from '../state/csrf';
import { ApiError } from './client';
import { useApi } from './context';

export interface MeUser {
  id: string;
  provider: string;
  email: string;
  name: string;
}

export interface MeSettings {
  favouriteClusters: string[] | null;
  watchIdleSeconds: number;
  theme: string;
}

export interface MeFeatures {
  kubeconfig: boolean;
  fileBrowser: boolean;
  nodeShell: boolean;
  vault: boolean;
  helmVersion: number;
  modules: Record<string, string>;
}

export interface MeLimits {
  fileTransferBytes: number;
  logsHistoryMaxLines: number;
  logsLiveBufferLines: number;
  vaultExpiryWarningSeconds: number;
}

/** The answer of `GET /api/v1/me`: who is signed in and what the server offers. */
export interface Me {
  user: MeUser;
  csrfToken: string;
  settings: MeSettings;
  maintenanceBanner: string;
  features: MeFeatures;
  limits: MeLimits;
  vault: unknown;
  keyId: string;
}

export type MeState =
  { state: 'loading' } | { state: 'signedIn'; me: Me } | { state: 'signedOut' } | { state: 'error' };

export function useMe(): MeState {
  const api = useApi();
  const query = useQuery({
    queryKey: ['me'],
    queryFn: async () => {
      const me = await api.get<Me>('/me');
      csrfStore.getState().setToken(me.csrfToken);
      return me;
    },
  });
  if (query.isPending) return { state: 'loading' };
  if (query.isError) {
    return query.error instanceof ApiError && query.error.status === 401
      ? { state: 'signedOut' }
      : { state: 'error' };
  }
  return { state: 'signedIn', me: query.data };
}

/** What the server offers (file browser, node shell, …); undefined until `me` has loaded. */
export function useFeatures(): MeFeatures | undefined {
  const me = useMe();
  return me.state === 'signedIn' ? me.me.features : undefined;
}
