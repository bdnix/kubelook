import { expect, test } from '@playwright/test';
import { cluster, discovery, setup } from './fixtures';
import { json } from './helpers';
import { installGuard } from './guard';

installGuard();

const served = (group: string, resource: string, kind: string) => ({
  group,
  version: 'v1alpha1',
  resource,
  kind,
  namespaced: true,
  verbs: ['get', 'list', 'watch', 'create', 'update', 'patch', 'delete'],
  preferred: true,
  servedVersions: ['v1alpha1'],
});

const withKeda = {
  ...discovery,
  resources: [
    ...discovery.resources,
    served('keda.sh', 'scaledobjects', 'ScaledObject'),
    served('keda.sh', 'scaledjobs', 'ScaledJob'),
  ],
};

const routes = (modules: unknown[]) => ({
  [`GET /clusters/${cluster.path}/discovery`]: (r: import('@playwright/test').Route) =>
    r.fulfill(json(withKeda)),
  [`GET /clusters/${cluster.path}/modules`]: (r: import('@playwright/test').Route) =>
    r.fulfill(json({ modules })),
});

const keda = (active: boolean) => ({
  name: 'keda',
  active,
  ...(active ? {} : { reason: 'disabled' }),
  kinds: active
    ? [
        {
          group: 'keda.sh',
          version: 'v1alpha1',
          resource: 'scaledobjects',
          kind: 'ScaledObject',
          namespaced: true,
        },
      ]
    : [],
});
const permissions = { name: 'permissions', active: true, kinds: [] };
const trivy = { name: 'trivy', active: false, reason: 'not_served', kinds: [] };

test('an active module shows in the sidebar and its kinds are absent from Custom Resources', async ({
  page,
}) => {
  await setup(page, routes([keda(true), permissions, trivy]));
  await page.goto(`/c/${cluster.path}/core/Pod`);

  const modules = page.getByRole('region', { name: 'Modules' });
  await expect(modules.getByRole('link', { name: 'KEDA' })).toBeVisible();
  await expect(
    modules.getByRole('group', { name: 'Security' }).getByRole('link', { name: 'Permissions and policy' }),
  ).toBeVisible();
  await expect(modules.getByRole('link', { name: 'Trivy Operator' })).toHaveCount(0);

  const custom = page.getByRole('region', { name: 'Custom Resources' });
  await expect(custom.getByRole('link', { name: 'ScaledJob' })).toBeVisible();
  await expect(custom.getByRole('link', { name: 'ScaledObject' })).toHaveCount(0);

  await modules.getByRole('link', { name: 'KEDA' }).click();
  await expect(page.getByRole('heading', { name: 'KEDA', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'ScaledObject' })).toBeVisible();
});

test('a disabled module is hidden and its kinds come back under Custom Resources', async ({ page }) => {
  await setup(page, routes([keda(false), permissions, trivy]));
  await page.goto(`/c/${cluster.path}/core/Pod`);

  await expect(
    page.getByRole('region', { name: 'Custom Resources' }).getByRole('link', { name: 'ScaledObject' }),
  ).toBeVisible();
  await expect(page.getByRole('region', { name: 'Modules' }).getByRole('link', { name: 'KEDA' })).toHaveCount(
    0,
  );

  await page.goto(`/c/${cluster.path}/modules/keda`);
  await expect(page.getByText('This cluster does not have this module.')).toBeVisible();
});
