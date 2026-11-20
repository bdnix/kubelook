import { expect, test } from '@playwright/test';
import { cluster, pod, setup } from './fixtures';
import { installGuard } from './guard';

installGuard();

const items = (d: Record<string, unknown>) =>
  d.resource === 'pods'
    ? [
        pod('1', 'web-1', 'Running'),
        pod('2', 'web-2', 'Pending'),
        pod('3', '=HYPERLINK("http://evil.example")', 'Running'),
      ]
    : [];

test('a query in the filter bar narrows the list and an invalid one shows its error', async ({ page }) => {
  await setup(page, {}, { items });
  await page.goto(`/c/${cluster.path}/core/Pod`);
  const grid = page.getByRole('grid', { name: 'Pods' });
  await expect(grid.getByText('web-1')).toBeVisible();

  const filter = page.getByLabel('Field filter');
  await filter.fill('status.phase = Pending');
  await expect(grid.getByText('web-2')).toBeVisible();
  await expect(grid.getByText('web-1')).toHaveCount(0);

  await filter.fill('status.phase =');
  await expect(page.getByRole('alert').filter({ hasText: 'at character' })).toBeVisible();
});

test('Export downloads a CSV in which a formula-like cell is neutralised', async ({ page }) => {
  await setup(page, {}, { items });
  await page.goto(`/c/${cluster.path}/core/Pod`);
  await expect(page.getByRole('grid', { name: 'Pods' }).getByText('web-1')).toBeVisible();

  await page.getByRole('button', { name: 'Export' }).click();
  const download = page.waitForEvent('download');
  await page.getByRole('menuitem', { name: 'CSV' }).click();
  const file = await download;
  expect(file.suggestedFilename()).toMatch(/\.csv$/);
  const stream = await file.createReadStream();
  const chunks: Buffer[] = [];
  for await (const chunk of stream) chunks.push(chunk as Buffer);
  const csv = Buffer.concat(chunks).toString('utf8');
  expect(csv).toContain('web-1');
  expect(csv).not.toMatch(/(^|,|\n)"?=HYPERLINK/);
  expect(csv).toContain("'=HYPERLINK");
});
