import { expect, test } from '@playwright/test';
import { cluster, pod, setup } from './fixtures';
import { installGuard } from './guard';
import { json } from './helpers';

installGuard();

const items = (d: Record<string, unknown>) =>
  d.resource === 'pods' ? [pod('1', 'web-1', 'Running'), pod('2', 'web-2', 'Running')] : [];

const job = (over: Record<string, unknown>) => ({
  id: 'j1',
  cluster: 'v:prod',
  type: 'drain',
  target: { name: 'node-a' },
  status: 'running',
  progress: { done: 1, total: 4, text: 'Evicting 3 pods' },
  message: '',
  offers: [],
  createdAt: '2026-11-20T00:00:00Z',
  updatedAt: '2026-11-20T00:00:01Z',
  ...over,
});

test('a bulk action on two rows names both objects in the confirmation and submits one request', async ({
  page,
}) => {
  let bulk: { action?: string; targets?: { name: string }[] } | undefined;
  await setup(
    page,
    {
      [`POST /clusters/${cluster.path}/bulk`]: (r) => {
        bulk = r.request().postDataJSON();
        return r.fulfill(json({ jobId: 'j9' }));
      },
      'GET /jobs': (r) => r.fulfill(json({ jobs: [] })),
    },
    { items },
  );
  await page.goto(`/c/${cluster.path}/core/Pod`);
  const grid = page.getByRole('grid', { name: 'Pods' });
  await grid.getByRole('checkbox', { name: 'Select web-1' }).click();
  await grid.getByRole('checkbox', { name: 'Select web-2' }).click();

  await page.getByRole('toolbar', { name: 'Bulk actions' }).getByRole('button', { name: 'Delete' }).click();
  const dialog = page.getByRole('dialog', { name: /Delete 2 Pods/ });
  await expect(dialog).toContainText('web-1');
  await expect(dialog).toContainText('web-2');
  await dialog.getByRole('button', { name: /Delete 2 objects/ }).click();
  await expect.poll(() => bulk?.action).toBe('delete');
  expect(bulk?.targets?.map((t) => t.name)).toEqual(['web-1', 'web-2']);
});

test('the jobs panel shows progress, and an interrupted job offers Retry', async ({ page }) => {
  let retried = false;
  await setup(
    page,
    {
      'GET /jobs': (r) =>
        r.fulfill(
          json({
            jobs: [
              job({}),
              job({
                id: 'j2',
                type: 'restart',
                target: { name: 'web' },
                status: 'interrupted',
                progress: { done: 0, total: 0, text: '' },
                message: 'The server restarted.',
                offers: ['retry'],
              }),
            ],
          }),
        ),
      'POST /jobs/j2/retry': (r) => {
        retried = true;
        return r.fulfill(json({ jobId: 'j3' }));
      },
    },
    { items },
  );
  await page.goto(`/c/${cluster.path}/core/Pod`);
  const jobs = page.getByRole('region', { name: 'Jobs' });
  await expect(jobs.getByRole('progressbar', { name: 'Drain node-a progress' })).toHaveAttribute(
    'aria-valuenow',
    '1',
  );
  await expect(jobs.getByText('Evicting 3 pods')).toBeVisible();
  await expect(jobs.getByText('The server restarted.')).toBeVisible();

  await jobs.getByRole('button', { name: 'Retry' }).click();
  await expect.poll(() => retried).toBe(true);
});
