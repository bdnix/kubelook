import { expect, test } from '@playwright/test';
import { cluster, pod, setup } from './fixtures';
import { installGuard } from './guard';
import { json } from './helpers';

installGuard();

const node = (name: string, ready: string, extra: Record<string, unknown> = {}) => ({
  apiVersion: 'v1',
  kind: 'Node',
  metadata: { uid: `uid-${name}`, name, resourceVersion: '1' },
  spec: extra,
  status: { conditions: [{ type: 'Ready', status: ready }] },
});

const crashing = {
  ...pod('2', 'crashy', 'Running'),
  status: {
    phase: 'Running',
    containerStatuses: [
      { name: 'app', ready: false, restartCount: 9, state: { waiting: { reason: 'CrashLoopBackOff' } } },
    ],
  },
};
const stuck = { ...pod('3', 'stuck', 'Pending'), status: { phase: 'Pending' } };

const items = (d: Record<string, unknown>) =>
  d.resource === 'pods'
    ? [pod('1', 'web-1', 'Running'), crashing, stuck]
    : d.resource === 'nodes'
      ? [node('n1', 'True'), node('n2', 'False', { unschedulable: true })]
      : d.resource === 'events'
        ? [
            {
              type: 'Warning',
              reason: 'FailedScheduling',
              message: '0/2 nodes are available',
              lastTimestamp: new Date(Date.now() - 120_000).toISOString(),
              involvedObject: { kind: 'Pod', name: 'stuck', namespace: 'shop' },
              metadata: { uid: 'e1', name: 'e1', namespace: 'shop', resourceVersion: '1' },
            },
          ]
        : [];

const routes = {
  [`GET /clusters/${cluster.path}/modules`]: (r: import('@playwright/test').Route) =>
    r.fulfill(json({ modules: [{ name: 'permissions', active: true, kinds: [] }] })),
  [`GET /clusters/${cluster.path}/helm/releases`]: (r: import('@playwright/test').Route) =>
    r.fulfill(
      json({
        releases: [
          { name: 'a', namespace: 'n', status: 'failed' },
          { name: 'b', namespace: 'n', status: 'deployed' },
        ],
      }),
    ),
};

test('the triage sections render from the lists, and a count opens the filtered list', async ({ page }) => {
  await setup(page, routes, { items });
  await page.goto(`/c/${cluster.path}`);

  const nodes = page.getByRole('region', { name: 'Nodes' });
  await expect(nodes).toContainText('Not ready 1');
  await expect(nodes).toContainText('Cordoned 1');
  const failing = page.getByRole('region', { name: 'Failing pods' });
  await expect(failing).toContainText('crashy');
  await expect(failing).toContainText('CrashLoopBackOff');
  await expect(page.getByRole('region', { name: 'Pending pods' })).toContainText('0/2 nodes are available');
  await expect(page.getByRole('region', { name: 'Failures' })).toContainText('1 failed Helm release');

  await page.getByRole('link', { name: '1 pending' }).click();
  await expect(page).toHaveURL(/\/core\/Pod\?filter=status\.phase/);
  await expect(page.getByRole('combobox', { name: 'Field filter' })).toHaveValue('status.phase = Pending');
  await expect(page.getByRole('grid', { name: 'Pods' }).getByText('stuck')).toBeVisible();
  await expect(page.getByRole('grid', { name: 'Pods' }).getByText('web-1')).toHaveCount(0);
});

test('leaving the page closes the watches it opened', async ({ page }) => {
  const socket = await setup(page, routes, { items });
  await page.goto(`/c/${cluster.path}`);
  await expect(page.getByRole('region', { name: 'Nodes' })).toBeVisible();
  const opens = () => socket.received.filter((m) => m.op === 'watch.open').length;
  const closes = () => socket.received.filter((m) => m.op === 'watch.close').length;
  expect(opens()).toBe(3);
  await page.getByRole('link', { name: 'Who can…' }).click();
  await expect(page).toHaveURL(/who-can$/);
  await expect.poll(closes).toBe(opens());
});
