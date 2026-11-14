import { QueryClient } from '@tanstack/react-query';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, renderApp } from '../../test/render';
import { JOBS_POLL_MS, type Job } from './useJobs';
import { JobsPanel } from './JobsPanel';

afterEach(() => vi.useRealTimers());

function job(over: Partial<Job> = {}): Job {
  return {
    id: 'j1',
    cluster: 'v:prod',
    type: 'drain',
    target: { name: 'node-1' },
    status: 'running',
    progress: { done: 1, total: 3, text: 'Evicted 1 of 3 pods' },
    message: '',
    offers: [],
    createdAt: '2026-11-14T12:00:00Z',
    updatedAt: '2026-11-14T12:00:01Z',
    ...over,
  };
}

/** A client that answers GET /jobs from a mutable list and counts the calls. */
function setup(initial: Job[]) {
  const state = { jobs: initial, gets: 0, posts: [] as string[] };
  const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const method = init?.method ?? 'GET';
    const path = String(input).replace('/api/v1', '');
    if (method === 'GET' && path.startsWith('/jobs')) {
      state.gets++;
      return Promise.resolve(jsonResponse({ jobs: state.jobs }));
    }
    state.posts.push(`${method} ${path}`);
    return Promise.resolve(jsonResponse({ jobId: 'j2' }));
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return { state, queryClient, client: createClient({ fetch }), fetch };
}

describe('JobsPanel', () => {
  it('shows nothing when the user has no jobs', async () => {
    const s = setup([]);
    renderApp(<JobsPanel />, { client: s.client, queryClient: s.queryClient });
    await waitFor(() => expect(s.state.gets).toBe(1));
    expect(screen.queryByRole('region', { name: 'Jobs' })).toBeNull();
  });

  it('lists jobs with their status, progress and message', async () => {
    const s = setup([
      job(),
      job({ id: 'j0', status: 'failed', message: 'Node not found', progress: { done: 0, total: 0, text: '' }, offers: ['retry'] }),
    ]);
    renderApp(<JobsPanel />, { client: s.client, queryClient: s.queryClient });
    const region = await screen.findByRole('region', { name: 'Jobs' });
    const rows = within(region).getAllByRole('listitem');
    expect(rows).toHaveLength(2);
    expect(within(rows[0]!).getByText('Running')).toBeInTheDocument();
    expect(within(rows[0]!).getByText('Evicted 1 of 3 pods')).toBeInTheDocument();
    expect(within(rows[0]!).getByRole('progressbar')).toHaveAttribute('aria-valuenow', '1');
    expect(within(rows[1]!).getByText('Failed')).toBeInTheDocument();
    expect(within(rows[1]!).getByText('Node not found')).toBeInTheDocument();
  });

  it('polls every 3 seconds only while a job is active', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const s = setup([job()]);
    renderApp(<JobsPanel />, { client: s.client, queryClient: s.queryClient });
    await screen.findByRole('region', { name: 'Jobs' });
    expect(s.state.gets).toBe(1);

    await vi.advanceTimersByTimeAsync(JOBS_POLL_MS + 50);
    await waitFor(() => expect(s.state.gets).toBe(2));

    s.state.jobs = [job({ status: 'succeeded' })];
    await vi.advanceTimersByTimeAsync(JOBS_POLL_MS + 50);
    await waitFor(() => expect(screen.getByText('Succeeded')).toBeInTheDocument());
    const after = s.state.gets;
    await vi.advanceTimersByTimeAsync(JOBS_POLL_MS * 5);
    expect(s.state.gets).toBe(after);
  });

  it('an interrupted job offers its buttons; Retry starts a new job and refreshes the list', async () => {
    const s = setup([
      job({ status: 'interrupted', message: 'Drain stopped part-way: 4 pods remain on the node.', offers: ['retry', 'rollback'] }),
    ]);
    renderApp(<JobsPanel />, { client: s.client, queryClient: s.queryClient });
    const region = await screen.findByRole('region', { name: 'Jobs' });
    expect(within(region).getByText('Interrupted')).toBeInTheDocument();
    expect(within(region).getByRole('button', { name: 'Rollback' })).toBeInTheDocument();
    s.state.jobs = [job({ id: 'j2', status: 'running' }), ...s.state.jobs];
    await userEvent.click(within(region).getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(s.state.posts).toEqual(['POST /jobs/j1/retry']));
    await waitFor(() => expect(within(region).getAllByRole('listitem')).toHaveLength(2));
  });

  it('other offers are handed to the caller', async () => {
    const onOffer = vi.fn();
    const j = job({ status: 'interrupted', offers: ['uninstall'] });
    const s = setup([j]);
    renderApp(<JobsPanel onOffer={onOffer} />, { client: s.client, queryClient: s.queryClient });
    await userEvent.click(await screen.findByRole('button', { name: 'Uninstall' }));
    expect(onOffer).toHaveBeenCalledWith(expect.objectContaining({ id: 'j1' }), 'uninstall');
    expect(s.state.posts).toEqual([]);
  });

  it('shows an error when Retry fails', async () => {
    const s = setup([job({ status: 'failed', offers: ['retry'] })]);
    const failing = createClient({
      fetch: (input, init) =>
        init?.method === 'POST'
          ? Promise.resolve(jsonResponse({ error: { code: 'conflict', message: 'Still running.' } }, 409))
          : s.fetch(input, init),
    });
    renderApp(<JobsPanel />, { client: failing, queryClient: s.queryClient });
    await userEvent.click(await screen.findByRole('button', { name: 'Retry' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Still running.');
  });

  it('lists the item results of a bulk job', async () => {
    const s = setup([
      job({
        type: 'bulk',
        status: 'failed',
        progress: {
          done: 2,
          total: 2,
          text: '1 of 2 failed',
          items: [
            { name: 'ns/a', status: 'done', message: '' },
            { name: 'ns/b', status: 'failed', message: 'forbidden' },
          ],
        },
      }),
    ]);
    renderApp(<JobsPanel />, { client: s.client, queryClient: s.queryClient });
    await screen.findByRole('region', { name: 'Jobs' });
    expect(screen.getByText('ns/b')).toBeInTheDocument();
    expect(screen.getByText('forbidden')).toBeInTheDocument();
  });
});
