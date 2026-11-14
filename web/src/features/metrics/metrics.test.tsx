import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, renderHook, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { ApiContext } from '../../api/context';
import { jsonResponse } from '../../test/render';
import { cpuText, memoryText, metricsColumns } from './columns';
import { Sparkline } from './Sparkline';
import { UsageBar } from './UsageBar';
import { METRICS_POLL_MS, useNodeMetrics, usePodMetrics } from './useMetrics';

afterEach(() => {
  vi.useRealTimers();
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
});

function setup(answer: () => unknown) {
  const urls: string[] = [];
  const fetch = vi.fn((url: string) => {
    urls.push(url);
    return Promise.resolve(jsonResponse(answer()));
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={new QueryClient()}>
      <ApiContext.Provider value={createClient({ fetch })}>{children}</ApiContext.Provider>
    </QueryClientProvider>
  );
  return { wrapper, urls, fetch };
}

describe('usePodMetrics and useNodeMetrics', () => {
  it('read the usage of the cluster, narrowed to a namespace', async () => {
    const { wrapper, urls } = setup(() => ({ available: true, items: [{ namespace: 'ns', name: 'web', cpuMillicores: 250, memoryBytes: 1024 }] }));
    const { result } = renderHook(() => usePodMetrics('v:prod', 'ns'), { wrapper });
    await waitFor(() => expect(result.current.data?.available).toBe(true));
    expect(urls[0]).toBe('/api/v1/clusters/v%3Aprod/metrics/pods?namespace=ns');
    expect(result.current.byKey.get('ns/web')).toMatchObject({ cpuMillicores: 250 });

    const nodes = setup(() => ({ available: true, items: [{ name: 'n1', cpuMillicores: 1, memoryBytes: 2 }] }));
    const n = renderHook(() => useNodeMetrics('v:prod'), { wrapper: nodes.wrapper });
    await waitFor(() => expect(n.result.current.data?.available).toBe(true));
    expect(nodes.urls[0]).toBe('/api/v1/clusters/v%3Aprod/metrics/nodes');
    expect(n.result.current.byKey.get('n1')).toMatchObject({ memoryBytes: 2 });
  });

  it('does nothing when it is switched off', () => {
    const { wrapper, fetch } = setup(() => ({ available: true, items: [] }));
    renderHook(() => usePodMetrics('v:prod', '', false), { wrapper });
    expect(fetch).not.toHaveBeenCalled();
  });

  it('polls every 30 seconds while the page is visible, and not while it is hidden', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { wrapper, fetch } = setup(() => ({ available: true, items: [] }));
    renderHook(() => usePodMetrics('v:prod', ''), { wrapper });
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    expect(METRICS_POLL_MS).toBe(30_000);
    await vi.advanceTimersByTimeAsync(METRICS_POLL_MS + 100);
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));

    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
    act(() => void document.dispatchEvent(new Event('visibilitychange')));
    await vi.advanceTimersByTimeAsync(METRICS_POLL_MS * 4);
    expect(fetch).toHaveBeenCalledTimes(2);

    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    act(() => void document.dispatchEvent(new Event('visibilitychange')));
    await vi.advanceTimersByTimeAsync(METRICS_POLL_MS + 100);
    await waitFor(() => expect(fetch.mock.calls.length).toBeGreaterThan(2));
  });

  it('an unavailable answer is data, not an error', async () => {
    const { wrapper } = setup(() => ({ available: false, items: [] }));
    const { result } = renderHook(() => usePodMetrics('v:prod', ''), { wrapper });
    await waitFor(() => expect(result.current.data?.available).toBe(false));
    expect(result.current.byKey.size).toBe(0);
  });
});

describe('metrics columns', () => {
  it('format CPU and memory and sort as numbers', () => {
    expect(cpuText(250)).toBe('250m');
    expect(cpuText(2000)).toBe('2');
    expect(cpuText(1500)).toBe('1.5');
    expect(memoryText(134217728)).toBe('128Mi');
    const byKey = new Map([['ns/web', { namespace: 'ns', name: 'web', cpuMillicores: 250, memoryBytes: 134217728 }]]);
    const cols = metricsColumns(byKey, 'pod');
    expect(cols.map((c) => [c.id, c.title, c.sort])).toEqual([
      ['cpuUsage', 'CPU', 'number'],
      ['memoryUsage', 'Memory', 'number'],
    ]);
    const pod = { metadata: { namespace: 'ns', name: 'web' } };
    expect(cols[0]?.value(pod, 0)).toBe(250);
    expect(cols[1]?.value({ metadata: { namespace: 'ns', name: 'none' } }, 0)).toBe('');
  });

  it('node columns are named for what they are next to the allocatable columns', () => {
    const cols = metricsColumns(new Map([['n1', { name: 'n1', cpuMillicores: 1500, memoryBytes: 1 << 30 }]]), 'node');
    expect(cols.map((c) => c.title)).toEqual(['CPU used', 'Memory used']);
    expect(cols[0]?.value({ metadata: { name: 'n1' } }, 0)).toBe(1500);
  });
});

describe('UsageBar', () => {
  it('is a meter with the percentage as text', () => {
    render(<UsageBar label="CPU" used={500} total={2000} format={(n) => `${n}m`} />);
    const meter = screen.getByRole('meter', { name: 'CPU' });
    expect(meter).toHaveAttribute('aria-valuenow', '25');
    expect(meter).toHaveAttribute('aria-valuemin', '0');
    expect(meter).toHaveAttribute('aria-valuemax', '100');
    expect(screen.getByText('500m of 2000m (25%)')).toBeInTheDocument();
  });

  it('caps at 100 and copes with nothing allocatable', () => {
    render(<UsageBar label="Memory" used={300} total={200} format={String} />);
    expect(screen.getByRole('meter', { name: 'Memory' })).toHaveAttribute('aria-valuenow', '100');
    render(<UsageBar label="Pods" used={1} total={0} format={String} />);
    expect(screen.getByRole('meter', { name: 'Pods' })).toHaveAttribute('aria-valuenow', '0');
    expect(screen.getByText('1 (allocatable unknown)')).toBeInTheDocument();
  });
});

describe('Sparkline', () => {
  it('draws the points as a line with a text alternative', () => {
    const { container } = render(<Sparkline label="CPU, last 60 minutes" points={[[1, 1], [2, 3], [3, 2]]} />);
    expect(screen.getByRole('img', { name: 'CPU, last 60 minutes' })).toBeInTheDocument();
    const line = container.querySelector('polyline');
    expect(line?.getAttribute('points')?.split(' ')).toHaveLength(3);
  });

  it('shows nothing to draw for too few points', () => {
    const { container } = render(<Sparkline label="x" points={[[1, 1]]} />);
    expect(container.querySelector('polyline')).toBeNull();
    expect(screen.getByText('No data')).toBeInTheDocument();
  });

  it('a flat line is drawn in the middle', () => {
    const { container } = render(<Sparkline label="x" points={[[1, 5], [2, 5]]} height={20} width={100} />);
    const ys = container.querySelector('polyline')?.getAttribute('points')?.split(' ').map((p) => Number(p.split(',')[1]));
    expect(ys).toEqual([10, 10]);
  });
});
