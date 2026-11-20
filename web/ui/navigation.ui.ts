import { expect, test } from '@playwright/test';
import { cluster, pod, setup } from './fixtures';
import { installGuard } from './guard';
import { json } from './helpers';

installGuard();

const items = (d: Record<string, unknown>) => (d.resource === 'pods' ? [pod('1', 'web-1', 'Running')] : []);

test('Ctrl+K opens the command palette and Enter goes to the chosen kind', async ({ page }) => {
  await setup(page, {}, { items });
  await page.goto(`/c/${cluster.path}/core/ConfigMap`);
  // the hotkey handler is mounted with the signed-in shell: wait for it
  await expect(page.getByRole('navigation', { name: 'Resources' })).toBeVisible();
  await page.keyboard.press('Control+k');

  const palette = page.getByRole('dialog', { name: 'Command palette' });
  await expect(palette).toBeVisible();
  await palette.getByRole('combobox', { name: 'Go to' }).fill('Pods');
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(new RegExp(`/c/v(:|%3A)prod/core/Pod$`));
  await expect(palette).toHaveCount(0);
});

test('a deep link to a cluster that is not in the session shows the message', async ({ page }) => {
  await setup(page, {
    'GET /clusters/other/discovery': (r) =>
      r.fulfill(
        json(
          {
            error: { code: 'cluster_unavailable', message: "This cluster isn't available in your session." },
          },
          404,
        ),
      ),
  });
  await page.goto('/c/other/core/Pod');
  await expect(
    page.getByText("This cluster isn't available in your session", { exact: false }),
  ).toBeVisible();
});
