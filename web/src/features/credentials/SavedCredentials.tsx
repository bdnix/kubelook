import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useState } from 'react';
import { useApi } from '../../api/context';
import { Button } from '../../components/Button';
import { vaultStatusStore } from '../../state/vaultStatus';
import { useVaultStore } from './context';
import type { SavedCredential } from './vaultStore';

const KIND_NAMES = { vault_token: 'Vault token', kubeconfig: 'Kubeconfig' } as const;

/** The settings section that lists the credentials saved in this browser and removes them. */
export function SavedCredentials() {
  const api = useApi();
  const store = useVaultStore();
  const queryClient = useQueryClient();
  const [items, setItems] = useState<SavedCredential[] | undefined>();

  const reload = useCallback(async () => setItems(await store.list()), [store]);
  useEffect(() => {
    void reload();
  }, [reload]);

  async function remove(item: SavedCredential) {
    try {
      await api.delete(
        item.kind === 'vault_token'
          ? '/credentials/vault'
          : `/credentials/kubeconfig/${encodeURIComponent(item.label)}`,
      );
    } catch {
      // The session may already be gone; the browser copy is removed regardless.
    }
    await store.remove(item.id);
    if (item.kind === 'vault_token') vaultStatusStore.getState().clear();
    await queryClient.invalidateQueries({ queryKey: ['clusters'] });
    await reload();
  }

  return (
    <section aria-label="Saved credentials">
      <h2>Saved credentials</h2>
      <p>
        Removing deletes only the copy saved in this browser and the credential in your current session. Other
        browsers keep their own copies.
      </p>
      {items?.length === 0 && <p>No credentials are saved in this browser.</p>}
      <ul>
        {items?.map((item) => (
          <li key={item.id}>
            <span>{KIND_NAMES[item.kind]}</span> <span>{item.label}</span>{' '}
            <small>Saved {item.savedAt.slice(0, 10)}</small>{' '}
            <Button aria-label={`Remove ${item.label}`} onClick={() => void remove(item)}>
              Remove
            </Button>
          </li>
        ))}
      </ul>
    </section>
  );
}
