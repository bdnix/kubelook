import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createClient } from './client';
import { ApiContext } from './context';
import { STATUS_POLL_MS, useStatus } from './status';
import { jsonResponse } from '../test/render';

afterEach(() => vi.useRealTimers());

describe('useStatus', () => {
  it('polls every 15 seconds', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let reachable = true;
    const fetch = vi.fn(() =>
      Promise.resolve(jsonResponse({ vault: { configured: true, reachable, since: null } })),
    );
    const client = createClient({ fetch });
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={new QueryClient()}>
        <ApiContext.Provider value={client}>{children}</ApiContext.Provider>
      </QueryClientProvider>
    );
    const { result } = renderHook(() => useStatus(), { wrapper });
    await waitFor(() => expect(result.current.data?.vault.reachable).toBe(true));
    reachable = false;
    await vi.advanceTimersByTimeAsync(STATUS_POLL_MS + 100);
    await waitFor(() => expect(result.current.data?.vault.reachable).toBe(false));
    expect(fetch.mock.calls.length).toBeGreaterThanOrEqual(2);
  });
});
