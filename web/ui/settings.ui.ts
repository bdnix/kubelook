import { expect, test } from '@playwright/test';
import { cluster, pod, setup } from './fixtures';
import { installGuard } from './guard';
import { json } from './helpers';

installGuard();

const items = (d: Record<string, unknown>) => (d.resource === 'pods' ? [pod('1', 'web-1', 'Running')] : []);

const settings = {
  favouriteClusters: [],
  namespaceFilters: {},
  columnPresets: {},
  filterSets: [],
  watchIdleSeconds: 30,
  theme: 'system',
};

test('changing the watch idle time saves the whole settings document', async ({ page }) => {
  let saved: { watchIdleSeconds?: number } | undefined;
  await setup(
    page,
    {
      'GET /settings': (r) => r.fulfill(json(settings)),
      'PUT /settings': (r) => {
        saved = r.request().postDataJSON();
        expect(r.request().headers()['x-csrf-token']).toBe('csrf-1');
        return r.fulfill(json({ ...settings, ...saved }));
      },
    },
    { items },
  );
  await page.goto('/settings');
  await page.getByLabel('Pause hidden live lists after').fill('120');
  await expect.poll(() => saved?.watchIdleSeconds).toBe(120);
});

test('replacing the Vault token does not close an open dock tab', async ({ page }) => {
  await setup(
    page,
    {
      'GET /settings': (r) => r.fulfill(json(settings)),
      'POST /credentials/vault': (r) =>
        r.fulfill(
          json({
            sealed: 's',
            vault: {
              displayName: 'bob',
              entityId: '',
              expiresAt: null,
              ttlSeconds: 3600,
              renewable: true,
              canRenew: true,
              neverExpires: false,
              warning: false,
              expired: false,
            },
          }),
        ),
    },
    {
      items,
      extra: (msg, send) => {
        if (msg.op === 'logs.open') {
          send({ ch: msg.ch, t: 'opened' });
          send({
            ch: msg.ch,
            t: 'lines',
            d: { lines: [{ ts: '2026-11-20T00:00:00Z', text: 'still running' }] },
          });
        }
      },
    },
  );
  await page.goto(`/c/${cluster.path}/ns/shop/core/Pod/web-1`);
  const drawer = page.getByRole('complementary', { name: 'Details' });
  await drawer.getByRole('tab', { name: 'Logs' }).click();
  await drawer.getByRole('button', { name: 'Open logs for web-1' }).click();
  const dock = page.getByRole('region', { name: 'Dock' });
  await expect(dock.getByText('still running')).toBeVisible();

  // client-side navigation: a reload would reset the dock
  await page.evaluate(() => {
    history.pushState({}, '', '/settings');
    dispatchEvent(new PopStateEvent('popstate'));
  });
  await page.getByLabel('Paste a new Vault token').fill('hvs.new-token');
  await page.getByRole('button', { name: 'Replace Vault token' }).click();
  await expect(dock.getByRole('tab', { name: 'Logs: web-1' })).toBeVisible();
  await expect(dock.getByText('still running')).toBeVisible();
});
