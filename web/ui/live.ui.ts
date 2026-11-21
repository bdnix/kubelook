import { expect, test, type Page } from '@playwright/test';
import { cluster, discovery, mockSocket, pod } from './fixtures';
import { installGuard } from './guard';
import { json, me, mockApi } from './helpers';

installGuard();

const items = (d: Record<string, unknown>) =>
  d.resource === 'pods' ? [pod('1', 'web-1', 'Running'), pod('2', 'web-2', 'Pending')] : [];

/** The shared socket stand-in, plus the log lines this file's logs test needs. */
const mockLive = (page: Page) =>
  mockSocket(page, {
    items,
    extra: (msg, send) => {
      if (msg.op === 'logs.open') {
        send({ ch: msg.ch, t: 'opened' });
        send({
          ch: msg.ch,
          t: 'lines',
          d: { lines: [{ ts: '2026-11-14T12:00:00Z', text: 'server listening on :8080' }] },
        });
      }
    },
  });

test('opens a cluster and shows its pods live, with status badges and updates from the server', async ({
  page,
}) => {
  let opened = false;
  await mockApi(page, {
    'GET /me': (r) => r.fulfill(json(me)),
    'GET /status': (r) => r.fulfill(json({ vault: { configured: true, reachable: true, since: null } })),
    'GET /clusters': (r) => r.fulfill(json(cluster.sources(opened ? 'open' : 'closed'))),
    'POST /clusters/v%3Aprod/open': (r) => {
      expect(r.request().headers()['x-csrf-token']).toBe('csrf-1');
      opened = true;
      return r.fulfill(json({ state: 'open', server: 'https://api.prod:6443', role: 'devops-role' }));
    },
    'GET /clusters/v%3Aprod/discovery': (r) => r.fulfill(json(discovery)),
  });
  const socket = await mockLive(page);
  // the overview opens its own watches first: the Pods list is on the channel the server opened for pods last
  const podsChannel = () =>
    socket.received.filter((m) => m.op === 'watch.open' && m.d?.resource === 'pods').at(-1)?.ch ?? 1;

  await page.goto('/');
  await page.getByRole('navigation', { name: 'Clusters' }).getByRole('link', { name: 'prod' }).click();

  await expect(page).toHaveURL(/\/c\/v(:|%3A)prod$/);
  await page.getByRole('navigation', { name: 'Resources' }).getByRole('link', { name: 'Pods' }).click();
  await expect(page).toHaveURL(/\/c\/v(:|%3A)prod\/core\/Pod$/);
  const grid = page.getByRole('grid', { name: 'Pods' });
  await expect(grid.getByText('web-1')).toBeVisible();
  await expect(grid.getByRole('img', { name: 'healthy' })).toBeVisible();
  await expect(grid.getByRole('img', { name: 'needs attention' })).toBeVisible();
  await expect(grid.getByText('1/2').first()).toBeVisible();

  socket.push({ ch: podsChannel(), t: 'event', d: { type: 'ADDED', object: pod('3', 'web-3', 'Running') } });
  await expect(grid.getByText('web-3')).toBeVisible();
  socket.push({
    ch: podsChannel(),
    t: 'event',
    d: { type: 'DELETED', object: pod('1', 'web-1', 'Running') },
  });
  await expect(grid.getByText('web-1')).toHaveCount(0);

  await page.getByRole('searchbox', { name: /Filter Pods/ }).fill('web-3');
  await expect(grid.getByText('web-2')).toHaveCount(0);
  await expect(grid.getByText('web-3')).toBeVisible();
});

test('a pod opens its logs in the dock, which survives navigating to another page', async ({ page }) => {
  await mockApi(page, {
    'GET /me': (r) => r.fulfill(json(me)),
    'GET /status': (r) => r.fulfill(json({ vault: { configured: true, reachable: true, since: null } })),
    'GET /clusters': (r) => r.fulfill(json(cluster.sources('open'))),
    'GET /clusters/v%3Aprod/discovery': (r) => r.fulfill(json(discovery)),
  });
  await mockLive(page);

  await page.goto('/c/v%3Aprod/core/Pod');
  const grid = page.getByRole('grid', { name: 'Pods' });
  await grid.getByText('web-1').dblclick();

  // the row opens the details drawer; its Logs tab opens the log in the dock
  await expect(page).toHaveURL(/\/c\/v(:|%3A)prod\/ns\/shop\/core\/Pod\/web-1$/);
  const drawer = page.getByRole('complementary', { name: 'Details' });
  await expect(drawer.getByRole('heading', { name: 'web-1' })).toBeVisible();
  await drawer.getByRole('tab', { name: 'Logs' }).click();
  await drawer.getByRole('button', { name: 'Open logs for web-1' }).click();

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
    'GET /clusters': (r) => r.fulfill(json(cluster.sources('closed'))),
  });
  await page.goto('/');
  await expect(page.getByRole('alert')).toContainText(
    'Open clusters keep working until their credentials expire',
  );
  await expect(
    page.getByRole('navigation', { name: 'Clusters' }).getByRole('button', { name: /^Open prod\./ }),
  ).toBeDisabled();
});
