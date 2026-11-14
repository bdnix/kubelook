import { useQuery } from '@tanstack/react-query';
import { useMemo } from 'react';
import { useApi } from '../../api/context';

export interface MetricItem {
  namespace?: string;
  name: string;
  cpuMillicores: number;
  memoryBytes: number;
  containers?: Array<{ name: string; cpuMillicores: number; memoryBytes: number }>;
}

export interface MetricsAnswer {
  available: boolean;
  items: MetricItem[];
}

export interface HistoryAnswer {
  available: boolean;
  series: Array<{ metric: 'cpu' | 'memory'; points: Array<[number, number]> }>;
  message?: string;
}

export const METRICS_POLL_MS = 30_000;

const base = (cluster: string) => `/clusters/${encodeURIComponent(cluster)}/metrics`;

/**
 * Polls metrics-server's numbers every 30 seconds. Polling stops while the page is hidden (React Query's
 * default) and starts again when it is shown.
 */
function useUsage(path: string, key: string, enabled: boolean, keyOf: (i: MetricItem) => string) {
  const api = useApi();
  const query = useQuery({
    queryKey: ['metrics', key],
    queryFn: () => api.get<MetricsAnswer>(path),
    enabled,
    refetchInterval: METRICS_POLL_MS,
    retry: false,
  });
  const byKey = useMemo(
    () => new Map((query.data?.items ?? []).map((i) => [keyOf(i), i])),
    [query.data, keyOf],
  );
  return { ...query, byKey };
}

const podKey = (i: MetricItem) => `${i.namespace ?? ''}/${i.name}`;
const nodeKey = (i: MetricItem) => i.name;

/** The CPU and memory of the pods, by `namespace/name`. */
export function usePodMetrics(cluster: string, namespace: string, enabled = true) {
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : '';
  return useUsage(`${base(cluster)}/pods${query}`, `${cluster}/pods/${namespace}`, enabled, podKey);
}

/** The CPU and memory of the nodes, by name. */
export function useNodeMetrics(cluster: string, enabled = true) {
  return useUsage(`${base(cluster)}/nodes`, `${cluster}/nodes`, enabled, nodeKey);
}

/** The last hour of a pod or node from Prometheus, when the cluster has one. */
export function useMetricsHistory(
  cluster: string,
  kind: 'pod' | 'node',
  namespace: string,
  name: string,
  minutes = 60,
  enabled = true,
) {
  const api = useApi();
  return useQuery({
    queryKey: ['metrics-history', cluster, kind, namespace, name, minutes],
    queryFn: () => {
      const q = new URLSearchParams({ kind, name, minutes: String(minutes) });
      if (namespace) q.set('namespace', namespace);
      return api.get<HistoryAnswer>(`${base(cluster)}/history?${q.toString()}`);
    },
    enabled,
    refetchInterval: METRICS_POLL_MS,
    retry: false,
  });
}
