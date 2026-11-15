import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from './client';
import { ApiContext } from './context';
import { useSettings, type Settings } from './settings';
import { jsonResponse } from '../test/render';

const base: Settings = {
  favouriteClusters: [],
  namespaceFilters: {},
  columnPresets: {},
  filterSets: [],
  watchIdleSeconds: 30,
  theme: 'system',
};

function setup(put: (body: Settings) => Response | Promise<Response>) {
  let server = base;
  const calls: string[] = [];
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    calls.push(`${init?.method ?? 'GET'} ${url}`);
    if ((init?.method ?? 'GET') === 'GET') return jsonResponse(server);
    const res = await put(JSON.parse(String(init?.body)) as Settings);
    if (res.ok) server = (await res.clone().json()) as Settings;
    return res;
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(['me'], {
    settings: { favouriteClusters: [], watchIdleSeconds: 30, theme: 'system' },
    user: { id: 'u1' },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <ApiContext.Provider value={createClient({ fetch })}>{children}</ApiContext.Provider>
    </QueryClientProvider>
  );
  return { wrapper, queryClient, calls };
}

describe('useSettings', () => {
  it('loads the settings', async () => {
    const { wrapper } = setup(() => jsonResponse(base));
    const { result } = renderHook(() => useSettings(), { wrapper });
    await waitFor(() => expect(result.current.settings).toEqual(base));
  });

  it('shows a change at once, sends the whole document and keeps the answer', async () => {
    let release: (r: Response) => void = () => undefined;
    const { wrapper, queryClient } = setup(() => new Promise<Response>((r) => (release = r)));
    const { result } = renderHook(() => useSettings(), { wrapper });
    await waitFor(() => expect(result.current.settings).toBeDefined());

    let done: Promise<boolean> = Promise.resolve(false);
    act(() => {
      done = result.current.update((s) => ({ ...s, favouriteClusters: ['v:a'], watchIdleSeconds: 90 }));
    });
    await waitFor(() => expect(result.current.settings?.favouriteClusters).toEqual(['v:a']));
    expect(result.current.saving).toBe(true);
    expect(
      (queryClient.getQueryData(['me']) as { settings: { favouriteClusters: string[] } }).settings
        .favouriteClusters,
    ).toEqual(['v:a']);

    release(jsonResponse({ ...base, favouriteClusters: ['v:a'], watchIdleSeconds: 90 }));
    await act(async () => {
      expect(await done).toBe(true);
    });
    expect(result.current.settings).toMatchObject({ favouriteClusters: ['v:a'], watchIdleSeconds: 90 });
    expect(result.current.saving).toBe(false);
    expect(result.current.error).toBeUndefined();
  });

  it('puts back what it showed when the server refuses, and says so', async () => {
    const { wrapper, queryClient } = setup(() =>
      jsonResponse(
        { error: { code: 'bad_request', message: 'watchIdleSeconds must be between 30 and 300.' } },
        400,
      ),
    );
    const { result } = renderHook(() => useSettings(), { wrapper });
    await waitFor(() => expect(result.current.settings).toBeDefined());
    let ok = true;
    await act(async () => {
      ok = await result.current.update((s) => ({ ...s, watchIdleSeconds: 10 }));
    });
    expect(ok).toBe(false);
    expect(result.current.settings?.watchIdleSeconds).toBe(30);
    expect(result.current.error).toBe('watchIdleSeconds must be between 30 and 300.');
    expect(
      (queryClient.getQueryData(['me']) as { settings: { watchIdleSeconds: number } }).settings
        .watchIdleSeconds,
    ).toBe(30);
  });

  it('changes made one after the other are all kept', async () => {
    const { wrapper } = setup((b) => jsonResponse(b));
    const { result } = renderHook(() => useSettings(), { wrapper });
    await waitFor(() => expect(result.current.settings).toBeDefined());
    await act(async () => {
      await result.current.update((s) => ({ ...s, theme: 'dark' }));
      await result.current.update((s) => ({ ...s, watchIdleSeconds: 60 }));
    });
    expect(result.current.settings).toMatchObject({ theme: 'dark', watchIdleSeconds: 60 });
  });
});
