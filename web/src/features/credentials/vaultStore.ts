import { openDB, type DBSchema, type IDBPDatabase } from 'idb';

/** A sealed copy of a credential, kept in this browser so it can be restored after sign-in. */
export interface SavedCredential {
  id: string;
  kind: 'vault_token' | 'kubeconfig';
  label: string;
  sealed: string;
  savedAt: string;
}

export interface VaultStore {
  put(item: SavedCredential): Promise<void>;
  list(): Promise<SavedCredential[]>;
  remove(id: string): Promise<void>;
  clear(): Promise<void>;
}

interface Row extends SavedCredential {
  key: string; // `${userId}/${id}`: two users of one browser never overwrite each other
  userId: string;
}

interface Schema extends DBSchema {
  credentials: { key: string; value: Row; indexes: { userId: string } };
}

const DB_NAME = 'kubelook';
const STORE = 'credentials';

function open(): Promise<IDBPDatabase<Schema>> {
  return openDB<Schema>(DB_NAME, 1, {
    upgrade(db) {
      db.createObjectStore(STORE, { keyPath: 'key' }).createIndex('userId', 'userId');
    },
  });
}

/**
 * The saved copies of one user. IndexedDB can be missing or cleared (private windows, Safari):
 * every operation then behaves as on an empty store.
 */
export function openVaultStore(userId: string): VaultStore {
  const run = async <T>(fallback: T, fn: (db: IDBPDatabase<Schema>) => Promise<T>): Promise<T> => {
    try {
      const db = await open();
      try {
        return await fn(db);
      } finally {
        db.close();
      }
    } catch {
      return fallback;
    }
  };
  return {
    put: (item) =>
      run(undefined, async (db) => {
        await db.put(STORE, { ...item, key: `${userId}/${item.id}`, userId });
      }),
    list: () =>
      run<SavedCredential[]>([], async (db) => {
        const rows = await db.getAllFromIndex(STORE, 'userId', userId);
        return rows
          .map(({ id, kind, label, sealed, savedAt }) => ({ id, kind, label, sealed, savedAt }))
          .sort((a, b) => a.id.localeCompare(b.id));
      }),
    remove: (id) =>
      run(undefined, async (db) => {
        await db.delete(STORE, `${userId}/${id}`);
      }),
    clear: () =>
      run(undefined, async (db) => {
        const keys = await db.getAllKeysFromIndex(STORE, 'userId', userId);
        await Promise.all(keys.map((k) => db.delete(STORE, k)));
      }),
  };
}
