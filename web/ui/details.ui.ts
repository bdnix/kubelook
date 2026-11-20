import { expect, test } from '@playwright/test';
import { cluster, pod, setup } from './fixtures';
import { installGuard } from './guard';

installGuard();

const items = (d: Record<string, unknown>) =>
  d.resource === 'pods' ? [pod('1', 'web-1', 'Running'), pod('2', 'web-2', 'Pending')] : [];

test('opening a row sets the deep link and the drawer has its six tabs', async ({ page }) => {
  await setup(page, {}, { items });
  await page.goto(`/c/${cluster.path}/core/Pod`);
  await page.getByRole('grid', { name: 'Pods' }).getByText('web-1').dblclick();

  await expect(page).toHaveURL(new RegExp(`/c/v(:|%3A)prod/ns/shop/core/Pod/web-1$`));
  const drawer = page.getByRole('complementary', { name: 'Details' });
  await expect(drawer.getByRole('heading', { name: 'web-1' })).toBeVisible();
  await expect(drawer.getByRole('tab')).toHaveText([
    'Overview',
    'YAML',
    'Events',
    'Logs',
    'Files',
    'Related',
  ]);
});

test('loading the deep link directly opens the drawer', async ({ page }) => {
  await setup(page, {}, { items });
  await page.goto(`/c/${cluster.path}/ns/shop/core/Pod/web-2`);
  await expect(
    page.getByRole('complementary', { name: 'Details' }).getByRole('heading', { name: 'web-2' }),
  ).toBeVisible();
});

test('Escape closes the drawer and returns to the list', async ({ page }) => {
  await setup(page, {}, { items });
  await page.goto(`/c/${cluster.path}/core/Pod`);
  await page.getByRole('grid', { name: 'Pods' }).getByText('web-1').dblclick();
  const drawer = page.getByRole('complementary', { name: 'Details' });
  await expect(drawer).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(drawer).toHaveCount(0);
  await expect(page).toHaveURL(new RegExp(`/c/v(:|%3A)prod/core/Pod$`));
  // the grid keeps DOM focus and names the row it is on through aria-activedescendant
  const grid = page.getByRole('grid', { name: 'Pods' });
  await expect(grid).toBeFocused();
  await expect(grid).toHaveAttribute('aria-activedescendant', /.+/);
});
