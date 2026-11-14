import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, type RenderResult } from '@testing-library/react';
import type { ReactElement, ReactNode } from 'react';
import { MemoryRouter } from 'react-router';
import { vi } from 'vitest';
import { ApiContext } from '../api/context';
import { createClient, type ApiClient } from '../api/client';
import { VaultStoreContext } from '../features/credentials/context';
import type { VaultStore } from '../features/credentials/vaultStore';

export function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

/** A client whose fetch answers from a table of "METHOD /path" → response factory. */
export function fakeClient(routes: Record<string, () => Response>): ApiClient {
  const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const key = `${init?.method ?? 'GET'} ${String(input).replace('/api/v1', '')}`;
    const make = routes[key];
    return Promise.resolve(make ? make() : jsonResponse({ error: { code: 'not_found', message: key } }, 404));
  });
  return createClient({ fetch });
}

export function makeWrapper(
  client: ApiClient = fakeClient({}),
  route = '/',
  vaultStore?: VaultStore,
  queryClient: QueryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } }),
) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <ApiContext.Provider value={client}>
          <VaultStoreContext.Provider value={vaultStore ?? null}>
            <MemoryRouter initialEntries={[route]}>{children}</MemoryRouter>
          </VaultStoreContext.Provider>
        </ApiContext.Provider>
      </QueryClientProvider>
    );
  };
}

export function renderApp(
  ui: ReactElement,
  {
    route = '/',
    client = fakeClient({}),
    vaultStore,
    queryClient,
  }: { route?: string; client?: ApiClient; vaultStore?: VaultStore; queryClient?: QueryClient } = {},
): RenderResult {
  return render(ui, { wrapper: makeWrapper(client, route, vaultStore, queryClient) });
}
