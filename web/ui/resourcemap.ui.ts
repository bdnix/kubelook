import { expect, test } from '@playwright/test';
import { cluster, deployment, setup } from './fixtures';
import { installGuard } from './guard';
import { json } from './helpers';

installGuard();

const graph = {
  nodes: [
    { id: 'Deployment/shop/web', group: 'apps', kind: 'Deployment', namespace: 'shop', name: 'web' },
    { id: 'Pod/shop/web-1', group: '', kind: 'Pod', namespace: 'shop', name: 'web-1', status: 'Failed' },
    { id: 'Pod/shop/web-2', group: '', kind: 'Pod', namespace: 'shop', name: 'web-2', status: 'Running' },
    { id: 'Service/shop/web', group: '', kind: 'Service', namespace: 'shop', name: 'web' },
  ],
  edges: [
    { from: 'Deployment/shop/web', to: 'Pod/shop/web-1', type: 'owns' },
    { from: 'Deployment/shop/web', to: 'Pod/shop/web-2', type: 'owns' },
    { from: 'Service/shop/web', to: 'Pod/shop/web-1', type: 'selects' },
  ],
  omittedKinds: ['Secret'],
  leftOutNamespaces: ['huge'],
};

const items = (d: Record<string, unknown>) => (d.resource === 'deployments' ? [deployment()] : []);

async function mapSocket(page: import('@playwright/test').Page) {
  const received: unknown[] = [];
  await setup(
    page,
    {
      'GET /settings': (r) =>
        r.fulfill(
          json({
            favouriteClusters: [],
            namespaceFilters: { [cluster.id]: ['shop'] },
            filterSets: [],
            columnPresets: {},
            theme: 'system',
            watchIdleSeconds: 30,
          }),
        ),
    },
    {
      items,
      extra: (msg, send) => {
        if (msg.op !== 'map.open') return;
        received.push(msg.d);
        send({ ch: msg.ch, t: 'opened' });
        send({ ch: msg.ch, t: 'graph', d: graph });
      },
    },
  );
  return received;
}

test('the map draws the nodes and edges of the graph, the legend, and names what was left out', async ({
  page,
}) => {
  const received = await mapSocket(page);
  await page.goto(`/c/${cluster.path}/resource-map`);

  await expect(page.getByRole('button', { name: 'Pod web-1, failed' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Pod web-2, running' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Deployment web' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Service web' })).toBeVisible();
  await expect(page.locator('svg line')).toHaveCount(3);
  await expect(page.getByRole('region', { name: 'Legend' })).toContainText('Failed pods are marked');
  await expect(page.getByText(/You may not list Secret/)).toBeVisible();
  await expect(page.getByText(/huge could not be shown/)).toBeVisible();
  expect(received[0]).toEqual({ cluster: cluster.id, namespaces: ['shop'] });
});

test('a click on a node opens its details', async ({ page }) => {
  await mapSocket(page);
  await page.goto(`/c/${cluster.path}/resource-map`);
  await page.getByRole('button', { name: 'Deployment web' }).click();
  await expect(page).toHaveURL(/\/ns\/shop\/apps\/Deployment\/web$/);
  const drawer = page.getByRole('complementary', { name: 'Details' });
  await expect(drawer.getByRole('tab', { name: 'Resources' })).toBeVisible();
  await drawer.getByRole('tab', { name: 'Resources' }).click();
  await expect(drawer.getByRole('region', { name: 'Related resources' })).toBeVisible();
  await expect(drawer.getByRole('button', { name: 'Pod web-1, failed' })).toBeVisible();
});
