import { expect, test } from '@playwright/test';
import { appsOpenApi, cluster, deployment, secret, setup } from './fixtures';
import { editorErrors, editorText, json } from './helpers';
import { installGuard } from './guard';

installGuard();

const dep = `/c/${cluster.path}/ns/shop/apps/Deployment/web`;
const base = `PUT /clusters/${cluster.path}/resources/apps/v1/deployments/web`;

const deploymentItems = (d: Record<string, unknown>) =>
  d.resource === 'deployments' ? [deployment()] : d.resource === 'secrets' ? [secret()] : [];

async function openYamlTab(page: import('@playwright/test').Page, url = dep) {
  await page.goto(url);
  const drawer = page.getByRole('complementary', { name: 'Details' });
  await drawer.getByRole('tab', { name: 'YAML' }).click();
  return drawer;
}

async function typeAtEnd(page: import('@playwright/test').Page, text: string) {
  await page.locator('.monaco-editor').first().click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type(text);
}

test('the YAML tab shows the object in Monaco and a change enables Preview, which shows the dry-run diff', async ({
  page,
}) => {
  let previewed = false;
  await setup(
    page,
    {
      [`GET /clusters/${cluster.path}/openapi/apps/v1`]: (r) => r.fulfill(json(appsOpenApi)),
      [base]: (r) => {
        previewed = true;
        const live = deployment('web', 2);
        const result = deployment('web', 3);
        return r.fulfill(json({ live, result }));
      },
    },
    { items: deploymentItems },
  );

  const drawer = await openYamlTab(page);
  await expect.poll(() => editorText(page)).toContain('replicas: 2');
  await expect(drawer.getByRole('button', { name: 'Preview' })).toBeDisabled();

  await typeAtEnd(page, '\n# edited');
  await expect(drawer.getByRole('button', { name: 'Preview' })).toBeEnabled();
  await drawer.getByRole('button', { name: 'Preview' }).click();
  await expect(drawer.getByLabel('Preview of the change')).toContainText('replicas: 3');
  expect(previewed).toBe(true);
});

test('Save sends a PUT with the loaded resourceVersion and the CSRF token', async ({ page }) => {
  let body: { metadata?: { resourceVersion?: string } } | undefined;
  let csrf: string | undefined;
  await setup(
    page,
    {
      [`GET /clusters/${cluster.path}/openapi/apps/v1`]: (r) => r.fulfill(json(appsOpenApi)),
      [base]: (r) => {
        body = r.request().postDataJSON();
        csrf = r.request().headers()['x-csrf-token'];
        return r.fulfill(
          json({ ...deployment('web', 2), metadata: { ...deployment().metadata, resourceVersion: '21' } }),
        );
      },
    },
    { items: deploymentItems },
  );

  const drawer = await openYamlTab(page);
  await expect.poll(() => editorText(page)).toContain('replicas: 2');
  await typeAtEnd(page, '\n# edited');
  await drawer.getByRole('button', { name: 'Save' }).click();
  await expect(drawer.getByText('Saved.')).toBeVisible();
  expect(csrf).toBe('csrf-1');
  expect(body?.metadata?.resourceVersion).toBe('20');
});

test('a 409 on Save shows the conflict banner', async ({ page }) => {
  await setup(
    page,
    {
      [`GET /clusters/${cluster.path}/openapi/apps/v1`]: (r) => r.fulfill(json(appsOpenApi)),
      [base]: (r) =>
        r.fulfill(json({ error: { code: 'conflict', message: 'The object has been modified.' } }, 409)),
    },
    { items: deploymentItems },
  );
  const drawer = await openYamlTab(page);
  await expect.poll(() => editorText(page)).toContain('replicas: 2');
  await typeAtEnd(page, '\n# edited');
  await drawer.getByRole('button', { name: 'Save' }).click();
  await expect(drawer.getByText('The object changed since you loaded it')).toBeVisible();
});

test('a Secret shows masked values in the editor', async ({ page }) => {
  await setup(
    page,
    {
      [`GET /clusters/${cluster.path}/openapi/core/v1`]: (r) =>
        r.fulfill(json({ openapi: '3.0.0', components: {} })),
    },
    { items: deploymentItems },
  );
  await openYamlTab(page, `/c/${cluster.path}/ns/shop/core/Secret/credentials`);
  await expect.poll(() => editorText(page)).toContain('password: "********"');
});

test('a value of the wrong type shows a validation marker from the schema', async ({ page }) => {
  await setup(
    page,
    { [`GET /clusters/${cluster.path}/openapi/apps/v1`]: (r) => r.fulfill(json(appsOpenApi)) },
    { items: (d) => (d.resource === 'deployments' ? [deployment('web', 'three' as unknown as number)] : []) },
  );
  await openYamlTab(page);
  await expect.poll(() => editorText(page)).toContain('replicas: three');
  await expect(editorErrors(page).first()).toBeVisible();
});
