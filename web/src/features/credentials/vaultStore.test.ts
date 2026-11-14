import 'fake-indexeddb/auto';
import { IDBFactory } from 'fake-indexeddb';
import { beforeEach, describe, expect, it } from 'vitest';
import { openVaultStore, type SavedCredential } from './vaultStore';

const vaultCopy: SavedCredential = {
  id: 'vault_token',
  kind: 'vault_token',
  label: 'oidc-ada',
  sealed: 'v1.aaaa.bbbb.cccc',
  savedAt: '2026-11-14T12:00:00Z',
};
const kubeCopy: SavedCredential = {
  id: 'kubeconfig:work',
  kind: 'kubeconfig',
  label: 'work',
  sealed: 'v1.dddd.eeee.ffff',
  savedAt: '2026-11-14T12:05:00Z',
};

beforeEach(() => {
  globalThis.indexedDB = new IDBFactory();
});

describe('vaultStore', () => {
  it('stores, lists, replaces and removes copies', async () => {
    const store = openVaultStore('u1');
    expect(await store.list()).toEqual([]);
    await store.put(vaultCopy);
    await store.put(kubeCopy);
    expect(await store.list()).toEqual([kubeCopy, vaultCopy].sort((a, b) => a.id.localeCompare(b.id)));

    await store.put({ ...vaultCopy, label: 'renamed' });
    expect((await store.list()).find((c) => c.id === 'vault_token')?.label).toBe('renamed');

    await store.remove('vault_token');
    expect(await store.list()).toEqual([kubeCopy]);
  });

  it('clear removes every copy of this user', async () => {
    const store = openVaultStore('u1');
    await store.put(vaultCopy);
    await store.put(kubeCopy);
    await store.clear();
    expect(await store.list()).toEqual([]);
  });

  it("does not list another user's copies, and does not remove them either", async () => {
    const first = openVaultStore('u1');
    const second = openVaultStore('u2');
    await first.put(vaultCopy);
    await second.put({ ...kubeCopy, id: 'kubeconfig:other' });
    expect((await first.list()).map((c) => c.id)).toEqual(['vault_token']);
    expect((await second.list()).map((c) => c.id)).toEqual(['kubeconfig:other']);

    await second.clear();
    expect((await first.list()).map((c) => c.id)).toEqual(['vault_token']);
  });

  it('uses the database "kubelook" with the object store "credentials"', async () => {
    await openVaultStore('u1').put(vaultCopy);
    const dbs = await indexedDB.databases();
    expect(dbs.map((d) => d.name)).toEqual(['kubelook']);
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const req = indexedDB.open('kubelook');
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
    expect([...db.objectStoreNames]).toEqual(['credentials']);
    db.close();
  });

  it('treats an unavailable IndexedDB as an empty store', async () => {
    globalThis.indexedDB = undefined as unknown as IDBFactory;
    const store = openVaultStore('u1');
    expect(await store.list()).toEqual([]);
    await expect(store.put(vaultCopy)).resolves.toBeUndefined();
    await expect(store.remove('x')).resolves.toBeUndefined();
    await expect(store.clear()).resolves.toBeUndefined();
  });
});
