import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useState } from 'react';
import { ApiError } from './client';
import { useApi } from './context';
import type { Me } from './me';

export interface FilterSet {
  name: string;
  kind: string;
  query: string;
}

/** The user's preferences: one document, kept on the server, shared by all their sessions. */
export interface Settings {
  favouriteClusters: string[];
  namespaceFilters: Record<string, string[]>;
  /** `<group>/<kind>` → the ids of the visible columns, in order. */
  columnPresets: Record<string, string[]>;
  filterSets: FilterSet[];
  watchIdleSeconds: number;
  theme: 'system' | 'light' | 'dark';
}

const KEY = ['settings'];

export interface SettingsApi {
  settings: Settings | undefined;
  /** Changes the settings: shown at once, saved as the whole document, put back when the server refuses. Resolves to whether it was saved. */
  update(change: (current: Settings) => Settings): Promise<boolean>;
  saving: boolean;
  error: string | undefined;
  /** False until the first answer (or failure) has arrived. */
  loaded: boolean;
}

export function useSettings(): SettingsApi {
  const api = useApi();
  const qc = useQueryClient();
  const query = useQuery({ queryKey: KEY, queryFn: () => api.get<Settings>('/settings'), retry: false });
  const [saving, setSaving] = useState(false);
  // A query that failed starts "pending" again when another component reads it: once settled, always loaded.
  const [settled, setSettled] = useState(false);
  useEffect(() => {
    if (!query.isPending) setSettled(true);
  }, [query.isPending]);
  const [error, setError] = useState<string>();

  // The "me" answer carries a few of the settings (favourites, theme): keep it in step.
  const patchMe = useCallback(
    (s: Settings) =>
      qc.setQueryData<Me>(['me'], (m) =>
        m
          ? {
              ...m,
              settings: {
                ...m.settings,
                favouriteClusters: s.favouriteClusters,
                watchIdleSeconds: s.watchIdleSeconds,
                theme: s.theme,
              },
            }
          : m,
      ),
    [qc],
  );

  const update = useCallback(
    async (change: (current: Settings) => Settings): Promise<boolean> => {
      const before = await qc.ensureQueryData({
        queryKey: KEY,
        queryFn: () => api.get<Settings>('/settings'),
      });
      const next = change(before);
      qc.setQueryData(KEY, next);
      patchMe(next);
      setSaving(true);
      setError(undefined);
      try {
        const saved = await api.put<Settings>('/settings', next);
        qc.setQueryData(KEY, saved);
        patchMe(saved);
        return true;
      } catch (e) {
        qc.setQueryData(KEY, before);
        patchMe(before);
        setError(e instanceof ApiError ? e.message : 'The settings could not be saved.');
        return false;
      } finally {
        setSaving(false);
      }
    },
    [api, qc, patchMe],
  );

  return { settings: query.data, update, saving, error, loaded: settled || !query.isPending };
}
