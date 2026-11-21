import { vi } from 'vitest';
import { createClient, type ApiClient } from '../../api/client';
import { jsonResponse } from '../../test/render';

export const release = (over: Record<string, unknown> = {}) => ({
  name: 'web',
  namespace: 'team-a',
  revision: 2,
  status: 'deployed',
  chart: 'web',
  chartVersion: '1.1.0',
  appVersion: '2.1',
  updated: '2026-11-20T10:00:00Z',
  applyMethod: 'csa',
  ...over,
});

export const detail = (over: Record<string, unknown> = {}) => ({
  ...release(),
  manifest: 'kind: ConfigMap\nmetadata:\n  name: web-config\n',
  notes: 'Thank you <b>for</b> installing web.\n',
  hooks: [{ name: 'web-test', kind: 'Pod', events: ['test'] }],
  history: [
    {
      revision: 2,
      status: 'deployed',
      chartVersion: '1.1.0',
      description: 'Upgrade complete',
      updated: '2026-11-20T10:00:00Z',
    },
    {
      revision: 1,
      status: 'superseded',
      chartVersion: '1.0.0',
      description: 'Install complete',
      updated: '2026-11-19T10:00:00Z',
    },
  ],
  valuesHidden: true,
  ...over,
});

export interface Call {
  method: string;
  path: string;
  body?: unknown;
}

/** An API that answers the Helm endpoints; `calls` lists every request. `allowed` is the answer of the access check. */
export function helmApi(
  opts: { allowed?: boolean; releases?: unknown[]; extra?: Record<string, () => Response> } = {},
) {
  const calls: Call[] = [];
  const base = '/clusters/c/helm';
  const routes: Record<string, (body: unknown) => Response> = {
    [`GET ${base}/releases`]: () => jsonResponse({ releases: opts.releases ?? [release()] }),
    [`GET ${base}/releases/team-a/web`]: () => jsonResponse(detail()),
    [`POST ${base}/releases/team-a/web/values/reveal`]: () =>
      jsonResponse({ userValues: 'replicas: 3\n', computedValues: 'replicas: 3\nmessage: hello\n' }),
    [`POST ${base}/preview`]: () =>
      jsonResponse({ currentManifest: 'replicas: "3"\n', newManifest: 'replicas: "5"\n' }),
    [`POST ${base}/jobs`]: () => jsonResponse({ jobId: 'job-1' }),
    [`GET ${base}/repositories`]: () =>
      jsonResponse({
        repositories: [{ name: 'charts', url: 'https://charts.example.org', scope: 'org', hasAuth: false }],
      }),
    [`POST ${base}/repositories`]: () => jsonResponse({ name: 'mine' }),
    [`DELETE ${base}/repositories/mine`]: () => jsonResponse({}),
    'POST /clusters/c/access': (body) =>
      jsonResponse({
        results: (body as { checks: unknown[] }).checks.map(() => ({
          allowed: opts.allowed ?? true,
          reason: 'no create on secrets',
        })),
      }),
    'GET /jobs': () => jsonResponse({ jobs: [] }),
    'GET /me': () =>
      jsonResponse({
        user: {},
        csrfToken: 'x',
        settings: {},
        maintenanceBanner: '',
        features: { helmVersion: 4, modules: {} },
        limits: {},
        vault: null,
        keyId: 'k',
      }),
    ...Object.fromEntries(Object.entries(opts.extra ?? {}).map(([k, v]) => [k, () => v()])),
  };
  const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input).replace('/api/v1', '');
    const method = init?.method ?? 'GET';
    const body = init?.body ? (JSON.parse(String(init.body)) as unknown) : undefined;
    calls.push({ method, path, body });
    const make = routes[`${method} ${path}`];
    return Promise.resolve(
      make ? make(body) : jsonResponse({ error: { code: 'not_found', message: `${method} ${path}` } }, 404),
    );
  });
  const client: ApiClient = createClient({ fetch });
  return { client, calls, fetch };
}
