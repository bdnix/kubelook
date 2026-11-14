import { useContext, useEffect, useMemo } from 'react';
import { useShallow } from 'zustand/react/shallow';
import { useStore } from 'zustand';
import { keyId, loadingEntry, type LiveEntry, type LiveKey } from './live';
import { appLiveStore, LiveContext } from './useLive';

/** Subscribes to one live list per key (one watch each) and merges them into one entry. */
export function useLiveMany(keys: LiveKey[] | undefined): LiveEntry {
  const store = useContext(LiveContext) ?? appLiveStore();
  const ids = (keys ?? []).map(keyId);
  const entries = useStore(
    store,
    useShallow((s) => ids.map((id) => s.entries[id])),
  );
  const signature = JSON.stringify(keys ?? []);
  useEffect(() => {
    if (!keys) return undefined;
    const leave = keys.map((k) => store.getState().subscribe(k));
    return () => leave.forEach((f) => f());
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keys are compared by their JSON
  }, [store, signature]);
  const none = !keys || keys.length === 0;
  // The merged entry is kept as long as no list changed: its items array must not be new on every render.
  return useMemo(() => {
    if (none) return loadingEntry;
    const known = entries.map((e) => e ?? loadingEntry);
    if (known.length === 1) return known[0] as LiveEntry;
    const failed = known.find((e) => e.status === 'error');
    if (failed) return failed;
    if (known.some((e) => e.status === 'loading')) return loadingEntry;
    return {
      status: known.some((e) => e.status === 'paused') ? ('paused' as const) : ('live' as const),
      items: known.flatMap((e) => e.items),
    };
  }, [none, entries]);
}
