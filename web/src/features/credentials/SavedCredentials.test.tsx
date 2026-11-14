import 'fake-indexeddb/auto';
import { QueryClient } from '@tanstack/react-query';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { IDBFactory } from 'fake-indexeddb';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { vaultStatusStore, type VaultStatus } from '../../state/vaultStatus';
import { fakeClient, renderApp } from '../../test/render';
import { SavedCredentials } from './SavedCredentials';
import { openVaultStore, type VaultStore } from './vaultStore';

let store: VaultStore;
beforeEach(async () => {
  globalThis.indexedDB = new IDBFactory();
  store = openVaultStore('u1');
  await store.put({
    id: 'vault_token',
    kind: 'vault_token',
    label: 'oidc-ada',
    sealed: 's1',
    savedAt: '2026-11-14T12:00:00Z',
  });
  await store.put({
    id: 'kubeconfig:my work',
    kind: 'kubeconfig',
    label: 'my work',
    sealed: 's2',
    savedAt: '2026-11-13T08:00:00Z',
  });
});

function spyClient() {
  const calls: string[] = [];
  const base = fakeClient({
    'DELETE /credentials/vault': () => new Response(null, { status: 204 }),
    'DELETE /credentials/kubeconfig/my%20work': () => new Response(null, { status: 204 }),
  });
  return {
    calls,
    client: { ...base, delete: <T,>(p: string, b?: unknown) => (calls.push(p), base.delete<T>(p, b)) },
  };
}

describe('SavedCredentials', () => {
  it('lists the saved copies with kind, label and date, and says removal affects this browser only', async () => {
    renderApp(<SavedCredentials />, { vaultStore: store });
    expect(await screen.findByText('oidc-ada')).toBeInTheDocument();
    expect(screen.getByText('my work')).toBeInTheDocument();
    expect(screen.getByText('Vault token')).toBeInTheDocument();
    expect(screen.getByText('Kubeconfig')).toBeInTheDocument();
    expect(screen.getByText(/2026-11-14/)).toBeInTheDocument();
    expect(screen.getByText(/only the copy saved in this browser/i)).toBeInTheDocument();
  });

  it('shows a message when nothing is saved', async () => {
    await store.clear();
    renderApp(<SavedCredentials />, { vaultStore: store });
    expect(await screen.findByText('No credentials are saved in this browser.')).toBeInTheDocument();
  });

  it('removing a Vault token deletes the copy, calls the endpoint and forgets the status', async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries');
    vaultStatusStore.getState().set({ displayName: 'x' } as VaultStatus);
    const { client, calls } = spyClient();
    renderApp(<SavedCredentials />, { client, vaultStore: store, queryClient });

    await user.click(await screen.findByRole('button', { name: 'Remove oidc-ada' }));
    await waitFor(() => expect(screen.queryByText('oidc-ada')).toBeNull());
    expect(calls).toEqual(['/credentials/vault']);
    expect((await store.list()).map((c) => c.id)).toEqual(['kubeconfig:my work']);
    expect(vaultStatusStore.getState().status).toBeNull();
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['clusters'] });
  });

  it('removing a kubeconfig percent-encodes its label in the endpoint', async () => {
    const user = userEvent.setup();
    const { client, calls } = spyClient();
    renderApp(<SavedCredentials />, { client, vaultStore: store });
    await user.click(await screen.findByRole('button', { name: 'Remove my work' }));
    await waitFor(() => expect(calls).toEqual(['/credentials/kubeconfig/my%20work']));
    expect((await store.list()).map((c) => c.id)).toEqual(['vault_token']);
  });

  it('removes the browser copy even when the server call fails', async () => {
    const user = userEvent.setup();
    renderApp(<SavedCredentials />, { client: fakeClient({}), vaultStore: store });
    await user.click(await screen.findByRole('button', { name: 'Remove oidc-ada' }));
    await waitFor(() => expect(screen.queryByText('oidc-ada')).toBeNull());
  });
});
