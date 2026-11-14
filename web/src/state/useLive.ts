import { createContext, useContext, useEffect } from 'react';
import { useStore } from 'zustand';
import { appSocket } from '../ws/context';
import { createLiveStore, keyId, loadingEntry, type LiveEntry, type LiveKey, type LiveStore } from './live';

let appStore: LiveStore | undefined;

/** The live store of the running app: one socket for the whole tab, created on first use. */
export function appLiveStore(): LiveStore {
  appStore ??= createLiveStore(appSocket());
  return appStore;
}

export const LiveContext = createContext<LiveStore | null>(null);

/** Subscribes the component to the live list of `key` for as long as it is mounted. */
export function useLive(key: LiveKey | undefined): LiveEntry {
  const store = useContext(LiveContext) ?? appLiveStore();
  const id = key ? keyId(key) : '';
  const entry = useStore(store, (s) => s.entries[id]);
  const { cluster, group, version, resource, namespace, fieldSelector } = key ?? {
    cluster: '',
    group: '',
    version: '',
    resource: '',
    namespace: '',
    fieldSelector: undefined,
  };
  const enabled = key !== undefined;
  useEffect(() => {
    if (!enabled) return undefined;
    return store.getState().subscribe({ cluster, group, version, resource, namespace, fieldSelector });
  }, [store, enabled, cluster, group, version, resource, namespace, fieldSelector]);
  return entry ?? loadingEntry;
}
