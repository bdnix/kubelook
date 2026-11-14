import 'fake-indexeddb/auto';
import { QueryClient } from '@tanstack/react-query';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { IDBFactory } from 'fake-indexeddb';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { vaultStatusStore, type VaultStatus } from '../../state/vaultStatus';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { VaultTokenForm } from './VaultTokenForm';
import { openVaultStore, type VaultStore } from './vaultStore';

const SECRET = 'hvs.VERY-SECRET-TOKEN';
const status: VaultStatus = {
  displayName: 'oidc-ada', entityId: 'e1', expiresAt: '2026-11-14T12:30:00Z', ttlSeconds: 1800, renewable: true,
  canRenew: true, neverExpires: false, warning: false, expired: false,
};

let store: VaultStore;
beforeEach(() => {
  globalThis.indexedDB = new IDBFactory();
  store = openVaultStore('u1');
  vaultStatusStore.getState().clear();
});

describe('VaultTokenForm', () => {
  it('has a password-type input and a disabled button until something is typed', async () => {
    renderApp(<VaultTokenForm />, { vaultStore: store });
    expect(screen.getByLabelText('Vault token')).toHaveAttribute('type', 'password');
    const button = screen.getByRole('button', { name: 'Add Vault token' });
    expect(button).toBeDisabled();
    await userEvent.setup().type(screen.getByLabelText('Vault token'), 'x');
    expect(button).toBeEnabled();
  });

  it('posts the token, keeps the sealed copy, remembers the status and clears the input', async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries');
    const client = fakeClient({
      'POST /credentials/vault': () => jsonResponse({ sealed: 'v1.sealed.copy.here', vault: status }),
    });
    const bodies: unknown[] = [];
    const spying = { ...client, post: <T,>(p: string, b?: unknown) => (bodies.push(b), client.post<T>(p, b)) };
    renderApp(<VaultTokenForm />, { client: spying, vaultStore: store, queryClient });

    await user.type(screen.getByLabelText('Vault token'), SECRET);
    await user.click(screen.getByRole('button', { name: 'Add Vault token' }));

    await waitFor(() => expect(vaultStatusStore.getState().status).toEqual(status));
    expect(bodies).toEqual([{ token: SECRET }]);
    const saved = await store.list();
    expect(saved).toHaveLength(1);
    expect(saved[0]).toMatchObject({ id: 'vault_token', kind: 'vault_token', label: 'oidc-ada', sealed: 'v1.sealed.copy.here' });
    expect(saved[0]?.savedAt).toMatch(/^\d{4}-\d{2}-\d{2}T/);
    expect(JSON.stringify(saved)).not.toContain(SECRET);
    expect(screen.getByLabelText('Vault token')).toHaveValue('');
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['clusters'] });
  });

  it('never writes the token to localStorage, the URL or the query cache', async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient();
    const setItem = vi.spyOn(Storage.prototype, 'setItem');
    const client = fakeClient({ 'POST /credentials/vault': () => jsonResponse({ sealed: 's', vault: status }) });
    renderApp(<VaultTokenForm />, { client, vaultStore: store, queryClient });

    await user.type(screen.getByLabelText('Vault token'), SECRET);
    await user.click(screen.getByRole('button', { name: 'Add Vault token' }));
    await waitFor(() => expect(vaultStatusStore.getState().status).not.toBeNull());

    expect(setItem).not.toHaveBeenCalled();
    expect(Object.values(localStorage).join()).not.toContain(SECRET);
    expect(window.location.href).not.toContain(SECRET);
    expect(queryClient.getMutationCache().getAll()).toHaveLength(0);
    const cached = JSON.stringify(queryClient.getQueryCache().getAll().map((q) => q.state.data));
    expect(cached).not.toContain(SECRET);
  });

  it("shows the server's message and keeps nothing when Vault rejects the token", async () => {
    const user = userEvent.setup();
    const client = fakeClient({
      'POST /credentials/vault': () =>
        jsonResponse({ error: { code: 'bad_request', message: 'Vault rejected this token.' } }, 400),
    });
    renderApp(<VaultTokenForm />, { client, vaultStore: store });
    await user.type(screen.getByLabelText('Vault token'), SECRET);
    await user.click(screen.getByRole('button', { name: 'Add Vault token' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Vault rejected this token.');
    expect(await store.list()).toEqual([]);
    expect(vaultStatusStore.getState().status).toBeNull();
    expect(screen.getByRole('button', { name: 'Add Vault token' })).toBeEnabled();
  });
});
