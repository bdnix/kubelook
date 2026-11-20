import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, makeWrapper } from '../../test/render';
import { useModules } from './useModules';

describe('useModules', () => {
  it('reads the modules of the cluster', async () => {
    const modules = [{ name: 'keda', active: true, kinds: [] }];
    const fetch = vi.fn(() => Promise.resolve(jsonResponse({ modules })));
    const { result } = renderHook(() => useModules('v:prod'), {
      wrapper: makeWrapper(createClient({ fetch })),
    });
    await waitFor(() => expect(result.current).toEqual(modules));
    expect(fetch).toHaveBeenCalledWith('/api/v1/clusters/v%3Aprod/modules', expect.anything());
  });

  it('is undefined while loading and empty after an error', async () => {
    const fetch = vi.fn(() =>
      Promise.resolve(jsonResponse({ error: { code: 'internal', message: 'x' } }, 500)),
    );
    const { result } = renderHook(() => useModules('c'), { wrapper: makeWrapper(createClient({ fetch })) });
    expect(result.current).toBeUndefined();
    await waitFor(() => expect(result.current).toEqual([]));
  });
});
