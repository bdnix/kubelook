import { useMutation, useQuery } from '@tanstack/react-query';
import { useCallback, useState } from 'react';
import { ApiError } from '../../api/client';
import { useApi } from '../../api/context';
import { useRefreshJobs } from '../jobs/useJobs';

export interface HelmRelease {
  name: string;
  namespace: string;
  revision: number;
  status: string;
  chart: string;
  chartVersion: string;
  appVersion: string;
  updated: string;
  applyMethod: string;
}

export interface HelmRevision {
  revision: number;
  status: string;
  chartVersion: string;
  description: string;
  updated: string;
}

export interface HelmHook {
  name: string;
  kind: string;
  events: string[];
}

/** A release as the details endpoint answers it: the values are never part of it. */
export interface HelmDetail extends HelmRelease {
  manifest: string;
  notes: string;
  hooks: HelmHook[];
  history: HelmRevision[];
  valuesHidden: boolean;
}

export interface HelmRepository {
  name: string;
  url: string;
  scope: 'org' | 'session';
  hasAuth: boolean;
}

export interface ChartRef {
  repo?: string;
  oci?: string;
  name?: string;
  version?: string;
}

export interface HelmAction {
  action: 'install' | 'upgrade' | 'rollback' | 'uninstall' | 'test';
  namespace: string;
  name: string;
  chart?: ChartRef;
  values?: Record<string, unknown>;
  revision?: number;
}

export const RELEASES_POLL_MS = 15_000;

const base = (cluster: string) => `/clusters/${encodeURIComponent(cluster)}/helm`;

/** Helm releases cannot be watched: the list is read again every 15 s while the tab is visible. */
export function useReleases(cluster: string) {
  const api = useApi();
  return useQuery({
    queryKey: ['helm', cluster, 'releases'],
    queryFn: async () => (await api.get<{ releases: HelmRelease[] }>(`${base(cluster)}/releases`)).releases,
    refetchInterval: RELEASES_POLL_MS,
    retry: false,
  });
}

export function useReleaseDetail(cluster: string, namespace: string, name: string) {
  const api = useApi();
  return useQuery({
    queryKey: ['helm', cluster, 'release', namespace, name],
    queryFn: () =>
      api.get<HelmDetail>(
        `${base(cluster)}/releases/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      ),
    retry: false,
  });
}

export interface Values {
  userValues: string;
  computedValues: string;
}

/** Reads the values of a release through the audited endpoint. */
export function useRevealValues(cluster: string, namespace: string, name: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (revision: number) =>
      api.post<Values>(
        `${base(cluster)}/releases/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/values/reveal`,
        { revision },
      ),
  });
}

export interface Preview {
  currentManifest: string;
  newManifest: string;
}

export function usePreview(cluster: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (body: HelmAction) => api.post<Preview>(`${base(cluster)}/preview`, body),
  });
}

/** Starts a Helm job (`POST …/helm/jobs`) and refreshes the job list so its progress row shows. */
export function useHelmJob(cluster: string) {
  const api = useApi();
  const refresh = useRefreshJobs();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const start = useCallback(
    async (body: HelmAction): Promise<boolean> => {
      setBusy(true);
      setError(undefined);
      try {
        await api.post(`${base(cluster)}/jobs`, body);
        await refresh();
        return true;
      } catch (e) {
        setError(e instanceof ApiError ? e.message : 'Something went wrong.');
        return false;
      } finally {
        setBusy(false);
      }
    },
    [api, cluster, refresh],
  );
  return { start, busy, error };
}

export function useRepositories(cluster: string) {
  const api = useApi();
  return useQuery({
    queryKey: ['helm', cluster, 'repositories'],
    queryFn: async () =>
      (await api.get<{ repositories: HelmRepository[] }>(`${base(cluster)}/repositories`)).repositories,
    retry: false,
  });
}

export interface NewRepository {
  name: string;
  url: string;
  username: string;
  password: string;
}

export function useAddRepository(cluster: string) {
  const api = useApi();
  return useMutation({ mutationFn: (r: NewRepository) => api.post(`${base(cluster)}/repositories`, r) });
}

export function useRemoveRepository(cluster: string) {
  const api = useApi();
  return useMutation({
    mutationFn: (name: string) => api.delete(`${base(cluster)}/repositories/${encodeURIComponent(name)}`),
  });
}
