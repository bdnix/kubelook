import { expect, test, type Page } from '@playwright/test';
import { json, me, mockApi } from './helpers';

const sources = (state: string) => ({
  sources: [
    {
      type: 'vault',
      label: 'Vault',
      status: 'ok',
      clusters: [{ id: 'v:prod', name: 'prod', state, roles: ['devops-role'] }],
    },
  ],
});

const discovery = {
  serverVersion: 'v1.35.0',
  resources: [
    {
      group: '',
      version: 'v1',
      resource: 'pods',
      kind: 'Pod',
      namespaced: true,
      verbs: ['list', 'watch'],
      preferred: true,
      servedVersions: ['v1'],
    },
  ],
};

const pod = (uid: string, name: string, phase: string) => ({
  metadata: {
    uid,
    name,
    namespace: 'shop',
    creationTimestamp: new Date(Date.now() - 3 * 3600_000).toISOString(),
  },
  spec: { nodeName: 'node-a', containers: [{ name: 'app' }, { name: 'sidecar' }] },
  status: {
    phase,
    containerStatuses: [{ ready: phase === 'Running', restartCount: 2, state: { running: {} } }],
  },
});

/** Plays the server of the WebSocket: answers watch.open with a snapshot and lets the test push events. */
async function mockSocket(page: Page) {
  const pushes: Array<(frame: unknown) => void> = [];
  await page.routeWebSocket('**/api/v1/ws', (ws) => {
    pushes.push((frame) => ws.send(JSON.stringify(frame)));
    ws.onMessage((raw) => {
      const msg = JSON.parse(String(raw)) as { op: string; ch: number };
      if (msg.op === 'watch.open') {
        ws.send(JSON.stringify({ ch: msg.ch, t: 'opened' }));
        ws.send(
          JSON.stringify({
            ch: msg.ch,
            t: 'snapshot',
            d: {
              items: [pod('1', 'web-1', 'Running'), pod('2', 'web-2', 'Pending')],
              resourceVersion: '5',
              more: false,
            },
          }),
        );
      }
      if (msg.op === 'logs.open') {
        ws.send(JSON.stringify({ ch: msg.ch, t: 'opened' }));
        ws.send(
          JSON.stringify({
            ch: msg.ch,
            t: 'lines',
            d: { lines: [{ ts: '2026-11-14T12:00:00Z', text: 'server listening on :8080' }] },
          }),
        );
      }
    });
  });
  return { push: (frame: unknown) => pushes.forEach((p) => p(frame)) };
}

test('opens a cluster and shows its pods live, with status badges and updates from the server', async ({
  page,
}) => {
  let opened = false;
  await mockApi(page, {
    'GET /me': (r) => r.fulfill(json(me)),
    'GET /status': (r) => r.fulfill(json({ vault: { configured: true, reachable: true, since: null } })),
    'GET /clusters': (r) => r.fulfill(json(sources(opened ? 'open' : 'closed'))),
    'POST /clusters/v%3Aprod/open': (r) => {
      expect(r.request().headers()['x-csrf-token']).toBe('csrf-1');
      opened = true;
      return r.fulfill(json({ state: 'open', server: 'https://api.prod:6443', role: 'devops-role' }));
    },
    'GET /clusters/v%3Aprod/discovery': (r) => r.fulfill(json(discovery)),
  });
  const socket = await mockSocket(page);

  await page.goto('/');
  await page.getByRole('navigation', { name: 'Clusters' }).getByRole('link', { name: 'prod' }).click();

  await expect(page).toHaveURL(/\/c\/v(:|%3A)prod\/core\/Pod$/);
  const grid = page.getByRole('grid', { name: 'Pods' });
  await expect(grid.getByText('web-1')).toBeVisible();
  await expect(grid.getByRole('img', { name: 'healthy' })).toBeVisible();
  await expect(grid.getByRole('img', { name: 'needs attention' })).toBeVisible();
  await expect(grid.getByText('1/2').first()).toBeVisible();

  socket.push({ ch: 1, t: 'event', d: { type: 'ADDED', object: pod('3', 'web-3', 'Running') } });
  await expect(grid.getByText('web-3')).toBeVisible();
  socket.push({ ch: 1, t: 'event', d: { type: 'DELETED', object: pod('1', 'web-1', 'Running') } });
  await expect(grid.getByText('web-1')).toHaveCount(0);

  await page.getByRole('searchbox', { name: /Filter Pods/ }).fill('web-3');
  await expect(grid.getByText('web-2')).toHaveCount(0);
  await expect(grid.getByText('web-3')).toBeVisible();
});

test('a pod opens its logs in the dock, which survives navigating to another page', async ({ page }) => {
  await mockApi(page, {
    'GET /me': (r) => r.fulfill(json(me)),
    'GET /status': (r) => r.fulfill(json({ vault: { configured: true, reachable: true, since: null } })),
    'GET /clusters': (r) => r.fulfill(json(sources('open'))),
    'GET /clusters/v%3Aprod/discovery': (r) => r.fulfill(json(discovery)),
  });
  await mockSocket(page);

  await page.goto('/c/v%3Aprod/core/Pod');
  const grid = page.getByRole('grid', { name: 'Pods' });
  await grid.getByText('web-1').dblclick();

  const dock = page.getByRole('region', { name: 'Dock' });
  await expect(dock.getByRole('tab', { name: 'Logs: web-1' })).toBeVisible();
  await expect(dock.getByText('server listening on :8080')).toBeVisible();

  await dock.getByLabel('Search').fill('listening');
  await expect(dock.locator('mark')).toHaveText('listening');

  // client-side navigation (a reload would reset the dock)
  await page.evaluate(() => {
    history.pushState({}, '', '/settings');
    dispatchEvent(new PopStateEvent('popstate'));
  });
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
  await expect(dock.getByRole('tab', { name: 'Logs: web-1' })).toBeVisible();

  await dock.getByRole('button', { name: 'Close Logs: web-1' }).click();
  await expect(dock.getByRole('tab')).toHaveCount(0);
});

test('while Vault is unreachable the banner shows and closed Vault clusters cannot be opened', async ({
  page,
}) => {
  await mockApi(page, {
    'GET /me': (r) => r.fulfill(json(me)),
    'GET /status': (r) =>
      r.fulfill(json({ vault: { configured: true, reachable: false, since: '2026-11-14T12:00:00Z' } })),
    'GET /clusters': (r) => r.fulfill(json(sources('closed'))),
  });
  await page.goto('/');
  await expect(page.getByRole('alert')).toContainText(
    'Open clusters keep working until their credentials expire',
  );
  await expect(
    page.getByRole('navigation', { name: 'Clusters' }).getByRole('button', { name: /^Open prod\./ }),
  ).toBeDisabled();
});
