import { expect, test } from '@playwright/test';
import { cluster, discovery, setup } from './fixtures';
import { installGuard } from './guard';
import { json } from './helpers';

installGuard();

const served = (group: string, resource: string, kind: string, version = 'v1') => ({
  group,
  version,
  resource,
  kind,
  namespaced: true,
  verbs: ['get', 'list', 'watch', 'create', 'update', 'patch', 'delete'],
  preferred: true,
  servedVersions: [version],
});

const withFlux = {
  ...discovery,
  resources: [
    ...discovery.resources,
    served('kustomize.toolkit.fluxcd.io', 'kustomizations', 'Kustomization'),
  ],
};

const created = new Date(Date.now() - 3600_000).toISOString();
const ks = (name: string, ready: string, extra: Record<string, unknown> = {}) => ({
  apiVersion: 'kustomize.toolkit.fluxcd.io/v1',
  kind: 'Kustomization',
  metadata: {
    uid: `uid-${name}`,
    name,
    namespace: 'flux-system',
    resourceVersion: '5',
    creationTimestamp: created,
  },
  spec: { sourceRef: { kind: 'GitRepository', name: 'repo' }, ...extra },
  status: {
    lastAppliedRevision: 'main@sha1:abc',
    conditions: [
      {
        type: 'Ready',
        status: ready,
        reason: 'X',
        message: ready === 'False' ? 'build failed' : 'applied',
        lastTransitionTime: created,
      },
    ],
  },
});

const controller = {
  metadata: {
    uid: 'c1',
    name: 'kustomize-controller',
    namespace: 'flux-system',
    labels: { 'app.kubernetes.io/part-of': 'flux', 'app.kubernetes.io/version': 'v2.7.0' },
  },
  spec: { replicas: 1 },
  status: { availableReplicas: 1 },
};

const items = (d: Record<string, unknown>) =>
  d.resource === 'kustomizations'
    ? [ks('apps', 'True'), ks('broken', 'False'), ks('paused', 'True', { suspend: true })]
    : d.resource === 'deployments'
      ? [controller]
      : [];

const routes = {
  [`GET /clusters/${cluster.path}/discovery`]: (r: import('@playwright/test').Route) =>
    r.fulfill(json(withFlux)),
  [`GET /clusters/${cluster.path}/modules`]: (r: import('@playwright/test').Route) =>
    r.fulfill(
      json({
        modules: [
          {
            name: 'flux',
            active: true,
            kinds: [
              {
                group: 'kustomize.toolkit.fluxcd.io',
                version: 'v1',
                resource: 'kustomizations',
                kind: 'Kustomization',
                namespaced: true,
              },
            ],
          },
        ],
      }),
    ),
  [`GET /clusters/${cluster.path}/flux/inventory`]: (r: import('@playwright/test').Route) => {
    const name = new URL(r.request().url()).searchParams.get('name');
    return r.fulfill(
      json({
        entries:
          name === 'apps'
            ? [{ group: 'apps', version: 'v1', kind: 'Deployment', namespace: 'shop', name: 'web' }]
            : [],
      }),
    );
  },
};

test('the dashboard counts the objects, and the graph renders nodes that open their details', async ({
  page,
}) => {
  await setup(page, routes, { items });
  await page.goto(`/c/${cluster.path}/flux`);

  const row = page.getByRole('row', { name: /Kustomization/ });
  await expect(row.getByRole('cell')).toHaveText(['Kustomization', '3', '1', '1', '0', '1']);
  await expect(page.getByRole('heading', { name: 'Flux v2.7.0' })).toBeVisible();
  await expect(page.getByRole('list', { name: 'Controllers' })).toContainText('kustomize-controller healthy');

  const broken = page.getByRole('button', { name: 'Kustomization broken, failed' });
  await expect(broken).toBeVisible();
  await expect(page.getByRole('button', { name: /Deployment web/ })).toBeVisible();
  await broken.focus();
  await expect(page.getByRole('tooltip')).toContainText('build failed');
  await broken.press('Enter');
  await expect(page).toHaveURL(/\/ns\/flux-system\/kustomize\.toolkit\.fluxcd\.io\/Kustomization\/broken$/);
  const drawer = page.getByRole('complementary', { name: 'Details' });
  await expect(drawer.getByRole('region', { name: 'Flux' })).toBeVisible();
  await expect(drawer.getByRole('button', { name: 'Reconcile' })).toBeEnabled();
});

test('Reconcile is disabled on a suspended object, with the reason', async ({ page }) => {
  await setup(page, routes, { items });
  await page.goto(`/c/${cluster.path}/ns/flux-system/kustomize.toolkit.fluxcd.io/Kustomization/paused`);
  const drawer = page.getByRole('complementary', { name: 'Details' });
  const reconcile = drawer.getByRole('button', { name: 'Reconcile' });
  await expect(reconcile).toBeDisabled();
  await expect(reconcile).toHaveAttribute('title', 'Resume the object before reconciling.');
  await expect(drawer.getByRole('button', { name: 'Resume' })).toBeEnabled();
});
