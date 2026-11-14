import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { useApi } from '../../api/context';
import { vaultStatusStore, type VaultStatus } from '../../state/vaultStatus';
import { useVaultStore } from './context';

export const KEY_CHANGED_NOTICE =
  "Kubelook's encryption key was changed, so your saved Vault token and kubeconfigs can't be used anymore. Please paste your token and upload your kubeconfigs again.";
export const TOKEN_EXPIRED_NOTICE = 'Your saved Vault token has expired. Paste a new token to continue.';

interface RestoreResult {
  status: 'ok' | 'key_changed' | 'wrong_user' | 'expired' | 'invalid' | 'vault_unreachable';
  kind?: string;
  vault?: VaultStatus;
}

export interface RestoreState {
  state: 'restoring' | 'done';
  notices: string[];
}

/** After sign-in: sends the copies saved in this browser to the server and prunes the ones it refuses. */
export function useRestore(): RestoreState {
  const api = useApi();
  const store = useVaultStore();
  const queryClient = useQueryClient();
  const [result, setResult] = useState<RestoreState>({ state: 'restoring', notices: [] });
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    void (async () => {
      const notices: string[] = [];
      try {
        const items = await store.list();
        if (items.length > 0) {
          const { results } = await api.post<{ results: RestoreResult[] }>('/credentials/restore', {
            items: items.map((i) => ({ sealed: i.sealed })),
          });
          for (const [i, r] of results.slice(0, items.length).entries()) {
            const item = items[i];
            if (!item) continue;
            switch (r.status) {
              case 'ok':
                if (r.vault) vaultStatusStore.getState().set(r.vault);
                break;
              case 'key_changed':
                await store.clear();
                if (!notices.includes(KEY_CHANGED_NOTICE)) notices.push(KEY_CHANGED_NOTICE);
                break;
              case 'expired':
                await store.remove(item.id);
                if (r.kind === 'vault_token' && !notices.includes(TOKEN_EXPIRED_NOTICE)) notices.push(TOKEN_EXPIRED_NOTICE);
                break;
              case 'wrong_user':
              case 'invalid':
                await store.remove(item.id);
                break;
              case 'vault_unreachable':
                break;
            }
          }
          await queryClient.invalidateQueries({ queryKey: ['clusters'] });
        }
      } catch {
        // The copies stay where they are; the user can still add credentials by hand.
      }
      setResult({ state: 'done', notices });
    })();
  }, [api, store, queryClient]);

  return result;
}
