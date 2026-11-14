import 'fake-indexeddb/auto';
import { QueryClient } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react';
import { IDBFactory } from 'fake-indexeddb';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { vaultStatusStore, type VaultStatus } from '../../state/vaultStatus';
import { fakeClient, jsonResponse, makeWrapper } from '../../test/render';
import { useVaultStore } from './context';
import { openVaultStore, type SavedCredential, type VaultStore } from './vaultStore';
import { KEY_CHANGED_NOTICE, TOKEN_EXPIRED_NOTICE, useRestore } from './useRestore';

const vaultCopy: SavedCredential = {
  id: 'vault_token',
  kind: 'vault_token',
  label: 'ada',
  sealed: 'S1',
  savedAt: '2026-11-14T12:00:00Z',
};
const kubeCopy: SavedCredential = {
  id: 'kubeconfig:work',
  kind: 'kubeconfig',
  label: 'work',
  sealed: 'S2',
  savedAt: '2026-11-14T12:00:00Z',
};
const status: VaultStatus = {
  displayName: 'ada',
  entityId: 'e1',
  expiresAt: '2026-11-14T12:30:00Z',
  ttlSeconds: 1800,
  renewable: true,
  canRenew: true,
  neverExpires: false,
  warning: false,
  expired: false,
};

let store: VaultStore;
beforeEach(() => {
  globalThis.indexedDB = new IDBFactory();
  store = openVaultStore('u1');
  vaultStatusStore.getState().clear();
});

function setup(results: object[] | (() => Response)) {
  const sent: unknown[] = [];
  const client = fakeClient({
    'POST /credentials/restore': () => {
      if (typeof results === 'function') return results();
      return jsonResponse({ results });
    },
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const invalidate = vi.spyOn(queryClient, 'invalidateQueries');
  const wrapper = makeWrapper(
    {
      ...client,
      post: (path, body) => {
        sent.push(body);
        return client.post(path, body);
      },
    },
    '/',
    store,
    queryClient,
  );
  const hook = renderHook(() => useRestore(), { wrapper });
  return { hook, sent, invalidate };
}

describe('useRestore', () => {
  it('does nothing when no copy is saved', async () => {
    const { hook, sent } = setup([]);
    await waitFor(() => expect(hook.result.current.state).toBe('done'));
    expect(sent).toEqual([]);
    expect(hook.result.current.notices).toEqual([]);
  });

  it('sends every saved copy, keeps the ok ones and remembers the Vault status', async () => {
    await store.put(vaultCopy);
    await store.put(kubeCopy);
    const { hook, sent, invalidate } = setup([
      { status: 'ok', kind: 'kubeconfig', label: 'work' },
      { status: 'ok', kind: 'vault_token', label: 'ada', vault: status },
    ]);
    expect(hook.result.current.state).toBe('restoring');
    await waitFor(() => expect(hook.result.current.state).toBe('done'));
    expect(sent).toEqual([{ items: [{ sealed: 'S2' }, { sealed: 'S1' }] }]);
    expect((await store.list()).length).toBe(2);
    expect(vaultStatusStore.getState().status).toEqual(status);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['clusters'] });
    expect(hook.result.current.notices).toEqual([]);
  });

  it('key_changed removes every copy and shows the notice once', async () => {
    await store.put(vaultCopy);
    await store.put(kubeCopy);
    const { hook } = setup([{ status: 'key_changed' }, { status: 'key_changed' }]);
    await waitFor(() => expect(hook.result.current.state).toBe('done'));
    expect(await store.list()).toEqual([]);
    expect(hook.result.current.notices).toEqual([KEY_CHANGED_NOTICE]);
    expect(KEY_CHANGED_NOTICE).toBe(
      "Kubelook's encryption key was changed, so your saved Vault token and kubeconfigs can't be used anymore. Please paste your token and upload your kubeconfigs again.",
    );
  });

  it('an expired Vault token is removed with an instruction to paste a new one', async () => {
    await store.put(vaultCopy);
    const { hook } = setup([{ status: 'expired', kind: 'vault_token' }]);
    await waitFor(() => expect(hook.result.current.state).toBe('done'));
    expect(await store.list()).toEqual([]);
    expect(hook.result.current.notices).toEqual([TOKEN_EXPIRED_NOTICE]);
  });

  it.each(['wrong_user', 'invalid'])('%s removes that copy silently', async (code) => {
    await store.put(vaultCopy);
    await store.put(kubeCopy);
    const { hook } = setup([{ status: code }, { status: 'ok', kind: 'vault_token', vault: status }]);
    await waitFor(() => expect(hook.result.current.state).toBe('done'));
    expect((await store.list()).map((c) => c.id)).toEqual(['vault_token']);
    expect(hook.result.current.notices).toEqual([]);
  });

  it('vault_unreachable keeps the copy', async () => {
    await store.put(vaultCopy);
    const { hook } = setup([{ status: 'vault_unreachable', kind: 'vault_token' }]);
    await waitFor(() => expect(hook.result.current.state).toBe('done'));
    expect((await store.list()).length).toBe(1);
    expect(hook.result.current.notices).toEqual([]);
  });

  it('keeps the copies when the restore request itself fails', async () => {
    await store.put(vaultCopy);
    const { hook } = setup(() => jsonResponse({ error: { code: 'internal', message: 'x' } }, 500));
    await waitFor(() => expect(hook.result.current.state).toBe('done'));
    expect((await store.list()).length).toBe(1);
  });

  it('ignores answers for copies it did not send (a short results list)', async () => {
    await store.put(vaultCopy);
    await store.put(kubeCopy);
    const { hook } = setup([{ status: 'ok', kind: 'kubeconfig' }]);
    await waitFor(() => expect(hook.result.current.state).toBe('done'));
    expect((await store.list()).length).toBe(2);
  });
});

describe('useVaultStore', () => {
  it('fails loudly without a provider', () => {
    expect(() => renderHook(() => useVaultStore())).toThrow('VaultStoreContext');
  });
});
