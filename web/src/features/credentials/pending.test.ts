import 'fake-indexeddb/auto';
import { IDBFactory } from 'fake-indexeddb';
import { beforeEach, describe, expect, it } from 'vitest';
import { fakeClient, jsonResponse } from '../../test/render';
import { syncPending } from './pending';
import { openVaultStore, type VaultStore } from './vaultStore';

let store: VaultStore;
beforeEach(() => {
  globalThis.indexedDB = new IDBFactory();
  store = openVaultStore('u1');
});

function setup(items: { label: string; sealed: string }[] | 'fail') {
  const acked: unknown[] = [];
  const client = fakeClient({
    'GET /credentials/pending': () =>
      items === 'fail' ? jsonResponse({ error: { code: 'internal', message: 'x' } }, 500) : jsonResponse({ items }),
    'POST /credentials/pending/ack': () => jsonResponse({}),
  });
  return {
    api: {
      ...client,
      post: (path: string, body?: unknown) => {
        if (path === '/credentials/pending/ack') acked.push(body);
        return client.post(path, body);
      },
    } as typeof client,
    acked,
  };
}

describe('syncPending', () => {
  it('replaces the saved copy of each label and then acknowledges those labels', async () => {
    await store.put({ id: 'kubeconfig:work', kind: 'kubeconfig', label: 'work', sealed: 'old', savedAt: '2026-11-01T00:00:00Z' });
    const { api, acked } = setup([{ label: 'work', sealed: 'new' }]);
    await syncPending(api, store);
    const [saved] = await store.list();
    expect(saved).toMatchObject({ id: 'kubeconfig:work', kind: 'kubeconfig', label: 'work', sealed: 'new' });
    expect(saved?.savedAt).not.toBe('2026-11-01T00:00:00Z');
    expect(acked).toEqual([{ labels: ['work'] }]);
  });

  it('does nothing, and acknowledges nothing, when no copy is pending', async () => {
    const { api, acked } = setup([]);
    await syncPending(api, store);
    expect(await store.list()).toEqual([]);
    expect(acked).toEqual([]);
  });

  it('keeps everything as it is when the pending request fails', async () => {
    await store.put({ id: 'kubeconfig:work', kind: 'kubeconfig', label: 'work', sealed: 'old', savedAt: 'x' });
    const { api, acked } = setup('fail');
    await expect(syncPending(api, store)).resolves.toBeUndefined();
    expect((await store.list())[0]?.sealed).toBe('old');
    expect(acked).toEqual([]);
  });
});
