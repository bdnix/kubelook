import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { fakeClient, jsonResponse, makeWrapper } from '../../test/render';
import { useClusters } from './useClusters';

describe('useClusters', () => {
  it('returns the sources of the session', async () => {
    const sources = [
      { type: 'vault', label: 'Vault', status: 'ok', clusters: [{ id: 'v:a', name: 'a', state: 'closed' }] },
    ];
    const client = fakeClient({ 'GET /clusters': () => jsonResponse({ sources }) });
    const { result } = renderHook(() => useClusters(), { wrapper: makeWrapper(client) });
    await waitFor(() => expect(result.current.data).toEqual(sources));
  });

  it('reports a failed request', async () => {
    const client = fakeClient({
      'GET /clusters': () => jsonResponse({ error: { code: 'internal', message: 'x' } }, 500),
    });
    const { result } = renderHook(() => useClusters(), { wrapper: makeWrapper(client) });
    await waitFor(() => expect(result.current.isError).toBe(true));
  });
});
