import { useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';
import { useApi } from '../../api/context';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { Field } from '../../components/Field';
import { vaultStatusStore, type VaultStatus } from '../../state/vaultStatus';
import { useVaultStore } from './context';
import { errorText } from './errors';

/**
 * Adds a Vault token. The token goes to the server once and is never kept here: not in a query
 * (no mutation cache), not in storage, not in the URL. Only the server's sealed copy is saved.
 */
export function VaultTokenForm() {
  const api = useApi();
  const store = useVaultStore();
  const queryClient = useQueryClient();
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      const { sealed, vault } = await api.post<{ sealed: string; vault: VaultStatus }>('/credentials/vault', {
        token,
      });
      await store.put({
        id: 'vault_token',
        kind: 'vault_token',
        label: vault.displayName,
        sealed,
        savedAt: new Date(Date.now()).toISOString(),
      });
      vaultStatusStore.getState().set(vault);
      setToken('');
      await queryClient.invalidateQueries({ queryKey: ['clusters'] });
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={(e) => void submit(e)} aria-label="Add a Vault token">
      <Field
        label="Vault token"
        type="password"
        autoComplete="off"
        value={token}
        onChange={(e) => setToken(e.target.value)}
        hint="Kubelook keeps the token for this session only. An encrypted copy can be saved in this browser."
      />
      <ErrorText message={error} />
      <Button type="submit" disabled={busy || token === ''}>
        Add Vault token
      </Button>
    </form>
  );
}
