import { expect, test } from '@playwright/test';
import { cluster, deployment, setup } from './fixtures';
import { installGuard } from './guard';
import { json } from './helpers';

installGuard();

const items = (d: Record<string, unknown>) => (d.resource === 'deployments' ? [deployment('web', 2)] : []);
const url = `/c/${cluster.path}/ns/shop/apps/Deployment/web`;

test('Scale validates the number and posts the action with the right body', async ({ page }) => {
  let body:
    | { action?: string; target?: { name?: string; resource?: string }; params?: { replicas?: number } }
    | undefined;
  await setup(
    page,
    {
      [`POST /clusters/${cluster.path}/actions`]: (r) => {
        body = r.request().postDataJSON();
        expect(r.request().headers()['x-csrf-token']).toBe('csrf-1');
        return r.fulfill(json({ ok: true }));
      },
    },
    { items },
  );
  await page.goto(url);
  await page.getByRole('button', { name: 'Actions' }).click();
  await page.getByRole('menuitem', { name: /Scale/ }).click();

  const dialog = page.getByRole('dialog', { name: 'Scale Deployment web' });
  const replicas = dialog.getByLabel('Replicas');
  await replicas.fill('-1');
  await expect(dialog.getByRole('button', { name: 'Scale', exact: true })).toBeDisabled();
  await expect(dialog.getByText('Enter zero or a positive whole number.')).toBeVisible();

  await replicas.fill('5');
  await dialog.getByRole('button', { name: 'Scale', exact: true }).click();
  await expect.poll(() => body?.action).toBe('scale');
  expect(body?.target?.name).toBe('web');
  expect(body?.target?.resource).toBe('deployments');
  expect(body?.params?.replicas).toBe(5);
});

test('an action the access check denies is disabled and says which permission is missing', async ({
  page,
}) => {
  await setup(
    page,
    {
      [`POST /clusters/${cluster.path}/access`]: async (r) => {
        const request = r.request().postDataJSON() as { checks: { verb: string }[] };
        await r.fulfill(
          json({
            results: request.checks.map((c) => ({ allowed: c.verb !== 'delete' && c.verb !== 'patch' })),
          }),
        );
      },
    },
    { items },
  );
  await page.goto(url);
  await page.getByRole('button', { name: 'Actions' }).click();
  const scale = page.getByRole('menuitem', { name: /Scale/ });
  await expect(scale).toBeDisabled();
  await expect(scale).toHaveAttribute('title', /patch/);
});

test('Delete asks for a confirmation that names the object', async ({ page }) => {
  await setup(page, {}, { items });
  await page.goto(url);
  await page.getByRole('button', { name: 'Actions' }).click();
  await page.getByRole('menuitem', { name: /Delete/ }).click();
  await expect(page.getByRole('dialog', { name: /Delete Deployment web/ })).toBeVisible();
});
