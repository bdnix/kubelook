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

// --- one list and its action per module ---

const ns = (group: string, resource: string, kind: string, version = 'v1alpha1') => ({
  group,
  version,
  resource,
  kind,
  namespaced: true,
  verbs: ['get', 'list', 'watch', 'create', 'update', 'patch', 'delete'],
  preferred: true,
  servedVersions: [version],
});

const moreKinds = [
  ns('keda.sh', 'scaledobjects', 'ScaledObject'),
  ns('external-secrets.io', 'externalsecrets', 'ExternalSecret', 'v1'),
  ns('aquasecurity.github.io', 'vulnerabilityreports', 'VulnerabilityReport'),
  {
    group: '',
    version: 'v1',
    resource: 'serviceaccounts',
    kind: 'ServiceAccount',
    namespaced: true,
    verbs: ['get', 'list', 'watch'],
    preferred: true,
    servedVersions: ['v1'],
  },
];

const moduleStatus = (
  name: string,
  kinds: { group: string; version: string; resource: string; kind: string }[],
) => ({
  name,
  active: true,
  kinds: kinds.map((k) => ({ ...k, namespaced: true })),
});

const createdAt = new Date(Date.now() - 3600_000).toISOString();
const meta = (name: string, extra: Record<string, unknown> = {}) => ({
  uid: `uid-${name}`,
  name,
  namespace: 'shop',
  resourceVersion: '2',
  creationTimestamp: createdAt,
  ...extra,
});

const moduleItems = (d: Record<string, unknown>) => {
  switch (d.resource) {
    case 'scaledobjects':
      return [
        {
          apiVersion: 'keda.sh/v1alpha1',
          kind: 'ScaledObject',
          metadata: meta('queue'),
          spec: { scaleTargetRef: { kind: 'Deployment', name: 'worker' }, triggers: [{ type: 'cpu' }] },
          status: { conditions: [{ type: 'Ready', status: 'True' }] },
        },
      ];
    case 'externalsecrets':
      return [
        {
          apiVersion: 'external-secrets.io/v1',
          kind: 'ExternalSecret',
          metadata: meta('db'),
          spec: { secretStoreRef: { name: 'vault' }, refreshInterval: '1h' },
          status: { conditions: [{ type: 'Ready', status: 'True', reason: 'SecretSynced' }] },
        },
      ];
    case 'vulnerabilityreports':
      return [
        {
          apiVersion: 'aquasecurity.github.io/v1alpha1',
          kind: 'VulnerabilityReport',
          metadata: meta('web-report'),
          report: {
            summary: { criticalCount: 1, highCount: 0, mediumCount: 0, lowCount: 0 },
            vulnerabilities: [
              {
                vulnerabilityID: 'CVE-9',
                resource: 'openssl',
                installedVersion: '1',
                fixedVersion: '2',
                severity: 'CRITICAL',
                title: 'bad',
              },
            ],
          },
        },
      ];
    case 'serviceaccounts':
      return [
        { apiVersion: 'v1', kind: 'ServiceAccount', metadata: meta('reader') },
        { apiVersion: 'v1', kind: 'ServiceAccount', metadata: meta('root') },
      ];
    default:
      return [];
  }
};

const rbac = (resource: string) => {
  const roles =
    resource === 'clusterroles'
      ? [
          {
            metadata: { name: 'view' },
            rules: [{ apiGroups: [''], resources: ['pods'], verbs: ['get', 'list'] }],
          },
          { metadata: { name: 'everything' }, rules: [{ apiGroups: ['*'], resources: ['*'], verbs: ['*'] }] },
        ]
      : resource === 'clusterrolebindings'
        ? [
            {
              metadata: { name: 'r' },
              roleRef: { kind: 'ClusterRole', name: 'view' },
              subjects: [{ kind: 'ServiceAccount', name: 'reader', namespace: 'shop' }],
            },
            {
              metadata: { name: 'x' },
              roleRef: { kind: 'ClusterRole', name: 'everything' },
              subjects: [{ kind: 'ServiceAccount', name: 'root', namespace: 'shop' }],
            },
          ]
        : [];
  return (r: import('@playwright/test').Route) => r.fulfill(json({ items: roles }));
};

const withModules = {
  ...discovery,
  resources: [...discovery.resources, ...moreKinds],
};

function moduleRoutes(actions: unknown[]) {
  const modules = [
    moduleStatus('keda', [
      { group: 'keda.sh', version: 'v1alpha1', resource: 'scaledobjects', kind: 'ScaledObject' },
    ]),
    moduleStatus('externalSecrets', [
      { group: 'external-secrets.io', version: 'v1', resource: 'externalsecrets', kind: 'ExternalSecret' },
    ]),
    moduleStatus('trivy', [
      {
        group: 'aquasecurity.github.io',
        version: 'v1alpha1',
        resource: 'vulnerabilityreports',
        kind: 'VulnerabilityReport',
      },
    ]),
    { name: 'permissions', active: true, kinds: [] },
  ];
  const base = `/clusters/${cluster.path}`;
  return {
    [`GET ${base}/discovery`]: (r: import('@playwright/test').Route) => r.fulfill(json(withModules)),
    [`GET ${base}/modules`]: (r: import('@playwright/test').Route) => r.fulfill(json({ modules })),
    [`POST ${base}/actions`]: (r: import('@playwright/test').Route) => {
      actions.push(r.request().postDataJSON());
      return r.fulfill(json({}));
    },
    [`GET ${base}/resources/rbac.authorization.k8s.io/v1/clusterroles`]: rbac('clusterroles'),
    [`GET ${base}/resources/rbac.authorization.k8s.io/v1/clusterrolebindings`]: rbac('clusterrolebindings'),
    [`GET ${base}/resources/rbac.authorization.k8s.io/v1/roles`]: rbac('roles'),
    [`GET ${base}/resources/rbac.authorization.k8s.io/v1/rolebindings`]: rbac('rolebindings'),
  };
}

test('KEDA pause and External Secrets force sync are actions of the details', async ({ page }) => {
  const actions: unknown[] = [];
  await setup(page, moduleRoutes(actions), { items: moduleItems });

  await page.goto(`/c/${cluster.path}/ns/shop/keda.sh/ScaledObject/queue`);
  const keda = page.getByRole('complementary', { name: 'Details' });
  await keda.getByRole('button', { name: 'Actions' }).click();
  await keda.getByRole('menuitem', { name: 'Pause' }).click();
  await expect.poll(() => actions.length).toBe(1);
  expect(actions[0]).toMatchObject({
    action: 'kedaPause',
    target: { group: 'keda.sh', resource: 'scaledobjects', name: 'queue' },
  });

  await page.goto(`/c/${cluster.path}/ns/shop/external-secrets.io/ExternalSecret/db`);
  const eso = page.getByRole('complementary', { name: 'Details' });
  await eso.getByRole('button', { name: 'Actions' }).click();
  await eso.getByRole('menuitem', { name: 'Force sync' }).click();
  await expect.poll(() => actions.length).toBe(2);
  expect(actions[1]).toMatchObject({ action: 'esoForceSync', target: { name: 'db' } });
});

test('a Trivy rescan asks first, and never shows matched secret text', async ({ page }) => {
  const actions: unknown[] = [];
  await setup(page, moduleRoutes(actions), { items: moduleItems });
  await page.goto(`/c/${cluster.path}/ns/shop/aquasecurity.github.io/VulnerabilityReport/web-report`);
  const drawer = page.getByRole('complementary', { name: 'Details' });
  await expect(drawer.getByRole('table', { name: 'Findings' })).toContainText('CVE-9');
  await drawer.getByRole('button', { name: 'Rescan' }).click();
  const dialog = page.getByRole('dialog', { name: 'Rescan web-report' });
  await expect(dialog).toContainText('The operator scans again after the report is deleted');
  expect(actions).toHaveLength(0);
  await dialog.getByRole('button', { name: 'Delete the report' }).click();
  await expect.poll(() => actions.length).toBe(1);
  expect(actions[0]).toMatchObject({
    action: 'trivyRescan',
    target: { resource: 'vulnerabilityreports', name: 'web-report' },
  });
});

test('the permissions page ranks ServiceAccounts by risk', async ({ page }) => {
  await setup(page, moduleRoutes([]), { items: moduleItems });
  await page.goto(`/c/${cluster.path}/modules/permissions`);
  await expect(page.getByText('Based on RBAC rules only.')).toBeVisible();
  const rows = page.getByRole('table', { name: 'ServiceAccounts' }).getByRole('row');
  await expect(rows.nth(1)).toContainText('critical');
  await expect(rows.nth(1)).toContainText('root');
  await expect(rows.nth(2)).toContainText('low');
  await expect(rows.nth(2)).toContainText('reader');
});
