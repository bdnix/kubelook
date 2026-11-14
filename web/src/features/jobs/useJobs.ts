import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useApi } from '../../api/context';

export type JobStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'interrupted';

export interface JobItem {
  name: string;
  status: 'done' | 'failed' | 'skipped';
  message?: string;
}

export interface Job {
  id: string;
  cluster: string;
  type: string;
  target: unknown;
  status: JobStatus;
  progress: { done: number; total: number; text: string; items?: JobItem[] };
  message: string;
  /** The buttons the server offers for a finished job: retry, rollback, uninstall. */
  offers: string[];
  createdAt: string;
  updatedAt: string;
  finishedAt?: string;
}

export const JOBS_POLL_MS = 3_000;

export const isActive = (j: Job) => j.status === 'queued' || j.status === 'running';

/** The user's jobs, polled every 3 s only while one of them is queued or running. */
export function useJobs() {
  const api = useApi();
  return useQuery({
    queryKey: ['jobs'],
    queryFn: async () => (await api.get<{ jobs: Job[] }>('/jobs')).jobs,
    refetchInterval: (query) => (query.state.data?.some(isActive) ? JOBS_POLL_MS : false),
    retry: false,
  });
}

/** Call after starting a job so the panel shows it and starts polling. */
export function useRefreshJobs() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: ['jobs'] });
}

/** Retry starts a new job from a failed or interrupted one. */
export function useRetryJob() {
  const api = useApi();
  const refresh = useRefreshJobs();
  return useMutation({
    mutationFn: (id: string) => api.post<{ jobId: string }>(`/jobs/${encodeURIComponent(id)}/retry`),
    onSuccess: () => refresh(),
  });
}
