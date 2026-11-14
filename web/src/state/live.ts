import { createStore } from 'zustand/vanilla';
import type { Channel, Socket } from '../ws/socket';

export type K8sObject = Record<string, unknown>;
export type LiveStatus = 'loading' | 'live' | 'paused' | 'error';

export interface LiveKey {
  cluster: string;
  /** The API group; `core` or empty for the core group. */
  group: string;
  version: string;
  resource: string;
  /** Empty for all namespaces. */
  namespace: string;
}

export interface LiveEntry {
  status: LiveStatus;
  items: K8sObject[];
  error?: { code: string; message: string };
}

export interface LiveState {
  entries: Record<string, LiveEntry>;
  /** Starts (or joins) the live list; returns the function that leaves it. */
  subscribe(key: LiveKey): () => void;
}

export const loadingEntry: LiveEntry = { status: 'loading', items: [] };

export function keyId(k: LiveKey): string {
  return [k.cluster, k.group || 'core', k.version, k.resource, k.namespace].join('|');
}

function uidOf(o: K8sObject): string {
  const meta = (o.metadata ?? {}) as { uid?: unknown; namespace?: unknown; name?: unknown };
  if (typeof meta.uid === 'string') return meta.uid;
  return `${String(meta.namespace ?? '')}/${String(meta.name ?? '')}`;
}

interface Sub {
  refs: number;
  channel: Channel | null;
  pending: K8sObject[] | null; // frames of a snapshot that is still arriving
  index: Map<string, number>;
}

/** The live lists of the open views. Two views of the same list share one channel. */
export function createLiveStore(socket: Socket) {
  const subs = new Map<string, Sub>();

  const store = createStore<LiveState>()((set) => {
    const update = (id: string, patch: (e: LiveEntry) => LiveEntry) =>
      set((s) => ({ entries: { ...s.entries, [id]: patch(s.entries[id] ?? loadingEntry) } }));

    const reindex = (sub: Sub, items: K8sObject[]) => {
      sub.index = new Map(items.map((o, i) => [uidOf(o), i]));
    };

    const onFrame = (id: string, key: LiveKey, sub: Sub, type: string, data: unknown) => {
      const d = (data ?? {}) as Record<string, unknown>;
      switch (type) {
        case 'snapshot': {
          const items = Array.isArray(d.items) ? (d.items as K8sObject[]) : [];
          sub.pending = [...(sub.pending ?? []), ...items];
          if (d.more === true) return;
          const all = sub.pending;
          sub.pending = null;
          reindex(sub, all);
          update(id, () => ({ status: 'live', items: all }));
          return;
        }
        case 'event': {
          const obj = d.object as K8sObject | undefined;
          if (!obj) return;
          update(id, (e) => {
            const uid = uidOf(obj);
            const at = sub.index.get(uid);
            if (d.type === 'DELETED') {
              if (at === undefined) return e;
              const items = e.items.filter((_, i) => i !== at);
              reindex(sub, items);
              return { ...e, items };
            }
            if (at === undefined) {
              sub.index.set(uid, e.items.length);
              return { ...e, items: [...e.items, obj] };
            }
            const items = e.items.slice();
            items[at] = obj;
            return { ...e, items };
          });
          return;
        }
        case 'paused':
          update(id, (e) => ({ ...e, status: 'paused' }));
          return;
        case 'error':
          update(id, (e) => ({
            ...e,
            status: 'error',
            error: {
              code: String(d.code ?? 'internal'),
              message: String(d.message ?? 'Something went wrong.'),
            },
          }));
          return;
        case 'closed':
          // The server dropped the channel (a browser that could not keep up): start over.
          if (d.reason === 'slow_consumer') open(id, key, sub);
          return;
      }
    };

    const open = (id: string, key: LiveKey, sub: Sub) => {
      sub.pending = null;
      sub.index = new Map();
      update(id, () => ({ status: 'loading', items: [] }));
      sub.channel = socket.open(
        'watch.open',
        {
          cluster: key.cluster,
          group: key.group || 'core',
          version: key.version,
          resource: key.resource,
          namespace: key.namespace,
        },
        { onFrame: (type, data) => onFrame(id, key, sub, type, data) },
      );
    };

    return {
      entries: {},
      subscribe(key) {
        const id = keyId(key);
        let sub = subs.get(id);
        if (!sub) {
          sub = { refs: 0, channel: null, pending: null, index: new Map() };
          subs.set(id, sub);
          open(id, key, sub);
        }
        sub.refs++;
        const joined = sub;
        let left = false;
        return () => {
          if (left) return;
          left = true;
          joined.refs--;
          if (joined.refs > 0) return;
          joined.channel?.close();
          subs.delete(id);
          set((s) => ({ entries: Object.fromEntries(Object.entries(s.entries).filter(([k]) => k !== id)) }));
        };
      },
    };
  });
  return store;
}

export type LiveStore = ReturnType<typeof createLiveStore>;
