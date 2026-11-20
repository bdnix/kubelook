import type { Page } from '@playwright/test';
import { json, me, mockApi } from './helpers';

/** One open Vault cluster "prod" (state "open"), the stand-in every screen test uses. */
export const cluster = {
  id: 'v:prod',
  path: 'v%3Aprod',
  sources: (state = 'open') => ({
    sources: [
      {
        type: 'vault',
        label: 'Vault',
        status: 'ok',
        clusters: [{ id: 'v:prod', name: 'prod', state, roles: ['devops-role'] }],
      },
    ],
  }),
};

const resource = (group: string, resource: string, kind: string, namespaced = true) => ({
  group,
  version: 'v1',
  resource,
  kind,
  namespaced,
  verbs: ['get', 'list', 'watch', 'create', 'update', 'patch', 'delete'],
  preferred: true,
  servedVersions: ['v1'],
});

/** What the cluster serves: the kinds the screen tests use. */
export const discovery = {
  serverVersion: 'v1.35.0',
  resources: [
    resource('', 'pods', 'Pod'),
    resource('', 'configmaps', 'ConfigMap'),
    resource('', 'secrets', 'Secret'),
    resource('', 'nodes', 'Node', false),
    resource('apps', 'deployments', 'Deployment'),
  ],
};

const created = () => new Date(Date.now() - 3 * 3600_000).toISOString();

export const pod = (uid: string, name: string, phase: string) => ({
  apiVersion: 'v1',
  kind: 'Pod',
  metadata: { uid, name, namespace: 'shop', resourceVersion: '10', creationTimestamp: created() },
  spec: { nodeName: 'node-a', containers: [{ name: 'app' }, { name: 'sidecar' }] },
  status: {
    phase,
    containerStatuses: [{ ready: phase === 'Running', restartCount: 2, state: { running: {} } }],
  },
});

export const deployment = (name = 'web', replicas = 2) => ({
  apiVersion: 'apps/v1',
  kind: 'Deployment',
  metadata: { uid: `uid-${name}`, name, namespace: 'shop', resourceVersion: '20', creationTimestamp: created() },
  spec: {
    replicas,
    selector: { matchLabels: { app: name } },
    template: { metadata: { labels: { app: name } }, spec: { containers: [{ name: 'app', image: 'web:1' }] } },
  },
  status: { replicas, readyReplicas: replicas, availableReplicas: replicas, updatedReplicas: replicas },
});

export const secret = () => ({
  apiVersion: 'v1',
  kind: 'Secret',
  type: 'Opaque',
  metadata: { uid: 'uid-s', name: 'credentials', namespace: 'shop', resourceVersion: '30', creationTimestamp: created() },
  data: { password: '********' },
});

/** An OpenAPI v3 document of apps/v1 in which Deployment.spec.replicas is an integer. */
export const appsOpenApi = {
  openapi: '3.0.0',
  components: {
    schemas: {
      'io.k8s.api.apps.v1.Deployment': {
        type: 'object',
        'x-kubernetes-group-version-kind': [{ group: 'apps', version: 'v1', kind: 'Deployment' }],
        properties: {
          apiVersion: { type: 'string' },
          kind: { type: 'string' },
          metadata: { type: 'object' },
          spec: {
            type: 'object',
            properties: { replicas: { type: 'integer' }, selector: { type: 'object' }, template: { type: 'object' } },
          },
        },
      },
    },
  },
};

type Frame = { ch: number; t: string; d?: unknown };
type SocketHandler = (msg: { op: string; ch: number; d?: Record<string, unknown> }, send: (f: Frame) => void) => void;

/**
 * Plays the server of the WebSocket. `watch.open` answers with `items(d)` as a snapshot; `extra` handles any
 * other op. The test can push frames and read what the page sent.
 */
export async function mockSocket(
  page: Page,
  opts: { items?: (d: Record<string, unknown>) => unknown[]; extra?: SocketHandler } = {},
) {
  const pushes: Array<(frame: Frame) => void> = [];
  const received: Array<{ op: string; ch: number; d?: Record<string, unknown> }> = [];
  await page.routeWebSocket('**/api/v1/ws', (ws) => {
    const send = (f: Frame) => ws.send(JSON.stringify(f));
    pushes.push(send);
    ws.onMessage((raw) => {
      const msg = JSON.parse(String(raw)) as { op: string; ch: number; d?: Record<string, unknown> };
      received.push(msg);
      if (msg.op === 'watch.open') {
        send({ ch: msg.ch, t: 'opened' });
        send({
          ch: msg.ch,
          t: 'snapshot',
          d: { items: (opts.items ?? (() => []))(msg.d ?? {}), resourceVersion: '5', more: false },
        });
        return;
      }
      opts.extra?.(msg, send);
    });
  });
  return { push: (frame: Frame) => pushes.forEach((p) => p(frame)), received };
}

/** The API calls every screen test needs: the user, the status, the cluster list, discovery and access. */
export function baseRoutes(extra: Record<string, (r: import('@playwright/test').Route) => unknown> = {}) {
  return {
    'GET /me': (r: import('@playwright/test').Route) => r.fulfill(json(me)),
    'GET /status': (r: import('@playwright/test').Route) =>
      r.fulfill(json({ vault: { configured: true, reachable: true, since: null } })),
    'GET /clusters': (r: import('@playwright/test').Route) => r.fulfill(json(cluster.sources())),
    [`GET /clusters/${cluster.path}/discovery`]: (r: import('@playwright/test').Route) =>
      r.fulfill(json(discovery)),
    [`POST /clusters/${cluster.path}/access`]: async (r: import('@playwright/test').Route) => {
      const body = r.request().postDataJSON() as { checks: unknown[] };
      await r.fulfill(json({ results: body.checks.map(() => ({ allowed: true })) }));
    },
    ...extra,
  };
}

export async function setup(
  page: Page,
  routes: Record<string, (r: import('@playwright/test').Route) => unknown> = {},
  socket: Parameters<typeof mockSocket>[1] = {},
) {
  await mockApi(page, baseRoutes(routes) as never);
  return mockSocket(page, socket);
}
