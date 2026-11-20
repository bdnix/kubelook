import { expect, test } from '@playwright/test';
import { cluster, discovery, setup } from './fixtures';
import { editorText, json } from './helpers';
import { installGuard } from './guard';

installGuard();

const withWidgets = {
  ...discovery,
  resources: [
    ...discovery.resources,
    {
      group: 'example.com',
      version: 'v1',
      resource: 'widgets',
      kind: 'Widget',
      namespaced: true,
      verbs: ['get', 'list', 'watch', 'create', 'update', 'patch', 'delete'],
      preferred: true,
      servedVersions: ['v1'],
    },
  ],
};

const crd = {
  spec: {
    group: 'example.com',
    names: { kind: 'Widget', plural: 'widgets' },
    scope: 'Namespaced',
    versions: [
      {
        name: 'v1',
        additionalPrinterColumns: [{ name: 'State', type: 'string', jsonPath: '.status.state' }],
        schema: {
          openAPIV3Schema: {
            type: 'object',
            properties: {
              spec: {
                type: 'object',
                properties: { size: { type: 'integer', description: 'How big the widget is.' } },
              },
            },
          },
        },
      },
    ],
  },
};

const widget = (name: string) => ({
  apiVersion: 'example.com/v1',
  kind: 'Widget',
  metadata: {
    uid: `uid-${name}`,
    name,
    namespace: 'shop',
    resourceVersion: '10',
    creationTimestamp: new Date(Date.now() - 3600_000).toISOString(),
  },
  spec: { size: 3 },
  status: { state: 'Ready' },
});

const routes = {
  [`GET /clusters/${cluster.path}/discovery`]: (r: import('@playwright/test').Route) =>
    r.fulfill(json(withWidgets)),
  [`GET /clusters/${cluster.path}/resources/apiextensions.k8s.io/v1/customresourcedefinitions/widgets.example.com`]:
    (r: import('@playwright/test').Route) => r.fulfill(json(crd)),
};
const items = (d: Record<string, unknown>) => (d.resource === 'widgets' ? [widget('w1')] : []);

test('a custom kind is listed under Custom Resources, with the printer column and its details', async ({
  page,
}) => {
  await setup(page, routes, { items });
  await page.goto(`/c/${cluster.path}/core/Pod`);

  const section = page.getByRole('region', { name: 'Custom Resources' });
  await expect(section.getByRole('group', { name: 'example.com' })).toBeVisible();
  await section.getByRole('link', { name: 'Widget' }).click();

  const grid = page.getByRole('grid', { name: 'Widget' });
  await expect(grid.getByRole('columnheader', { name: /State/ })).toBeVisible();
  await expect(grid.getByText('Ready')).toBeVisible();

  await grid.getByText('w1').dblclick();
  const drawer = page.getByRole('complementary', { name: 'Details' });
  await expect(
    drawer.getByRole('region', { name: 'Spec' }).getByText('How big the widget is.'),
  ).toBeVisible();
});

test('Create from YAML opens with a template of the kind, and a created object appears in the list', async ({
  page,
}) => {
  const socket = await setup(
    page,
    {
      ...routes,
      [`POST /clusters/${cluster.path}/apply`]: (r) =>
        r.fulfill(
          json({
            results: [{ index: 0, kind: 'Widget', namespace: 'default', name: 'example', action: 'created' }],
          }),
        ),
    },
    { items },
  );
  await page.goto(`/c/${cluster.path}/example.com/Widget`);
  await expect(page.getByRole('grid', { name: 'Widget' }).getByText('w1')).toBeVisible();

  await page.getByRole('button', { name: 'Create from YAML' }).click();
  await expect
    .poll(() => editorText(page))
    .toContain('apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: example\n  namespace: default');
  await page.getByRole('button', { name: 'Create', exact: true }).click();
  await expect(page.getByRole('list', { name: 'Results' })).toContainText('Widget default/example: created');

  const channel = socket.received.find((m) => m.op === 'watch.open' && m.d?.resource === 'widgets')?.ch;
  expect(channel).toBeDefined();
  socket.push({ ch: channel as number, t: 'event', d: { type: 'ADDED', object: widget('example') } });
  await expect(
    page.getByRole('grid', { name: 'Widget' }).getByText('example', { exact: true }),
  ).toBeVisible();
});
