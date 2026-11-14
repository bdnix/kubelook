import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from './client';
import { ApiContext } from './context';
import { useAccess } from './access';
import { findResource, useDiscovery, type Discovery } from './discovery';
import { useOpenCluster } from './open';
import { jsonResponse } from '../test/render';

const discovery: Discovery = {
  serverVersion: 'v1.35.0',
  resources: [
    {
      group: '',
      version: 'v1',
      resource: 'pods',
      kind: 'Pod',
      namespaced: true,
      verbs: ['list'],
      preferred: true,
      servedVersions: ['v1'],
    },
    {
      group: 'example.io',
      version: 'v1beta1',
      resource: 'widgets',
      kind: 'Widget',
      namespaced: true,
      verbs: [],
      preferred: false,
      servedVersions: ['v1', 'v1beta1'],
    },
    {
      group: 'example.io',
      version: 'v1',
      resource: 'widgets',
      kind: 'Widget',
      namespaced: true,
      verbs: [],
      preferred: true,
      servedVersions: ['v1', 'v1beta1'],
    },
  ],
};

function wrap(routes: Record<string, () => Response>) {
  const calls: string[] = [];
  const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${String(input).replace('/api/v1', '')}`;
    calls.push(key);
    const make = routes[key];
    return Promise.resolve(make ? make() : jsonResponse({ error: { code: 'not_found', message: key } }, 404));
  });
  const client = createClient({ fetch });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <ApiContext.Provider value={client}>{children}</ApiContext.Provider>
    </QueryClientProvider>
  );
  return { Wrapper, calls };
}

describe('discovery', () => {
  it('reads the cluster with the ID percent-encoded', async () => {
    const { Wrapper, calls } = wrap({ 'GET /clusters/k%3Aa%2Fb/discovery': () => jsonResponse(discovery) });
    const { result } = renderHook(() => useDiscovery('k:a/b'), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(calls).toEqual(['GET /clusters/k%3Aa%2Fb/discovery']);
  });

  it('does not ask while disabled', () => {
    const { Wrapper, calls } = wrap({});
    renderHook(() => useDiscovery('v:prod', false), { wrapper: Wrapper });
    expect(calls).toEqual([]);
  });

  it('finds the preferred version; core and empty group are the same', () => {
    expect(findResource(discovery, 'core', 'Pod')?.resource).toBe('pods');
    expect(findResource(discovery, '', 'Pod')?.resource).toBe('pods');
    expect(findResource(discovery, 'example.io', 'Widget')?.version).toBe('v1');
    expect(findResource(discovery, 'example.io', 'Nope')).toBeUndefined();
  });
});

describe('access', () => {
  it('posts the checks and returns the results in order', async () => {
    const checks = [
      { verb: 'list', group: '', resource: 'pods', namespace: 'ns' },
      { verb: 'delete', group: '', resource: 'pods' },
    ];
    const { Wrapper, calls } = wrap({
      'POST /clusters/v%3Aprod/access': () =>
        jsonResponse({
          results: [{ allowed: true }, { allowed: false, reason: 'missing permission: delete pods' }],
        }),
    });
    const { result } = renderHook(() => useAccess('v:prod', checks), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([
      { allowed: true },
      { allowed: false, reason: 'missing permission: delete pods' },
    ]);
    expect(calls).toEqual(['POST /clusters/v%3Aprod/access']);
  });

  it('asks nothing for no checks or when disabled', () => {
    const { Wrapper, calls } = wrap({});
    renderHook(() => useAccess('v:prod', []), { wrapper: Wrapper });
    renderHook(() => useAccess('v:prod', [{ verb: 'get', group: '', resource: 'pods' }], false), {
      wrapper: Wrapper,
    });
    expect(calls).toEqual([]);
  });
});

const sources = (state: string) => ({
  sources: [
    { type: 'vault', label: 'Vault', status: 'ok', clusters: [{ id: 'v:prod', name: 'prod', state }] },
  ],
});

describe('useOpenCluster', () => {
  it('is open at once for a cluster that is already open, without calling open', async () => {
    const { Wrapper, calls } = wrap({ 'GET /clusters': () => jsonResponse(sources('open')) });
    const { result } = renderHook(() => useOpenCluster('v:prod'), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.status).toBe('open'));
    expect(calls.some((c) => c.startsWith('POST'))).toBe(false);
  });

  it('opens a closed cluster, then refreshes the cluster list', async () => {
    let state = 'closed';
    const { Wrapper, calls } = wrap({
      'GET /clusters': () => jsonResponse(sources(state)),
      'POST /clusters/v%3Aprod/open': () => {
        state = 'open';
        return jsonResponse({ state: 'open', server: 'https://api' });
      },
    });
    const { result } = renderHook(() => useOpenCluster('v:prod'), { wrapper: Wrapper });
    expect(result.current.status).toBe('opening');
    await waitFor(() => expect(result.current.status).toBe('open'));
    expect(calls.filter((c) => c === 'GET /clusters').length).toBeGreaterThanOrEqual(2);
  });

  it('reports the error when opening fails', async () => {
    const { Wrapper } = wrap({
      'GET /clusters': () => jsonResponse(sources('closed')),
      'POST /clusters/v%3Aprod/open': () =>
        jsonResponse(
          {
            error: {
              code: 'vault_unreachable',
              message: "Vault is unreachable, so this cluster can't be opened right now",
            },
          },
          503,
        ),
    });
    const { result } = renderHook(() => useOpenCluster('v:prod'), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.status).toBe('error'));
    expect(result.current).toMatchObject({
      error: { message: expect.stringContaining('Vault is unreachable') },
    });
  });
});
