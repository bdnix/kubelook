import { expect, test } from '@playwright/test';
import { cluster, discovery, setup } from './fixtures';
import { installGuard } from './guard';
import { json } from './helpers';

installGuard();

const served = (group: string, resource: string, kind: string, namespaced: boolean, version = 'v1') => ({
  group,
  version,
  resource,
  kind,
  namespaced,
  verbs: ['get', 'list', 'watch', 'create', 'update', 'patch', 'delete'],
  preferred: true,
  servedVersions: [version],
});

const withKarpenter = {
  ...discovery,
  resources: [
    ...discovery.resources,
    served('karpenter.sh', 'nodepools', 'NodePool', false),
    served('karpenter.sh', 'nodeclaims', 'NodeClaim', false),
  ],
};

const node = (name: string, pool: string, zone: string, type: string) => ({
  apiVersion: 'v1',
  kind: 'Node',
  metadata: {
    uid: `uid-${name}`,
    name,
    resourceVersion: '3',
    labels: {
      'karpenter.sh/nodepool': pool,
      'topology.kubernetes.io/zone': zone,
      'node.kubernetes.io/instance-type': type,
      'karpenter.sh/capacity-type': 'spot',
    },
  },
  status: {
    capacity: { cpu: '4', memory: '16Gi', pods: '58' },
    conditions: [{ type: 'Ready', status: 'True' }],
  },
});

const pool = (name: string) => ({
  apiVersion: 'karpenter.sh/v1',
  kind: 'NodePool',
  metadata: { uid: `uid-${name}`, name, resourceVersion: '4' },
  spec: { limits: { cpu: '16' }, template: { spec: { nodeClassRef: { name: 'ec2' }, expireAfter: '720h' } } },
  status: { nodes: 2, conditions: [{ type: 'Ready', status: 'True' }] },
});

const items = (d: Record<string, unknown>) =>
  d.resource === 'nodes'
    ? [
        node('a', 'default', 'z1', 'm5.large'),
        node('b', 'default', 'z2', 'm5.large'),
        node('c', 'batch', 'z1', 'c5.large'),
      ]
    : d.resource === 'nodepools'
      ? [pool('default'), pool('batch')]
      : [];

const routes = {
  [`GET /clusters/${cluster.path}/discovery`]: (r: import('@playwright/test').Route) =>
    r.fulfill(json(withKarpenter)),
  [`GET /clusters/${cluster.path}/modules`]: (r: import('@playwright/test').Route) =>
    r.fulfill(
      json({
        modules: [
          {
            name: 'karpenter',
            active: true,
            kinds: [
              {
                group: 'karpenter.sh',
                version: 'v1',
                resource: 'nodepools',
                kind: 'NodePool',
                namespaced: false,
              },
            ],
          },
        ],
      }),
    ),
};

test('the overview shows a card per NodePool with its limits, and the treemap regroups when the grouping changes', async ({
  page,
}) => {
  await setup(page, routes, { items });
  await page.goto(`/c/${cluster.path}/modules/karpenter`);

  const totals = page.getByRole('region', { name: 'Totals' });
  await expect(totals).toContainText('Nodes 3');
  await expect(totals).toContainText('Managed by Karpenter 3');
  const card = page.getByRole('region', { name: 'NodePool default' });
  await expect(card).toContainText('ec2');
  await expect(card).toContainText('m5.large');
  await expect(card.getByRole('progressbar', { name: 'CPU' })).toHaveAttribute('aria-valuemax', '16');
  await expect(page.getByRole('region', { name: 'NodePool batch' })).toBeVisible();

  await page.getByRole('tab', { name: 'Topology' }).click();
  const map = page.getByRole('img', { name: 'Node topology' });
  await expect(map.locator('rect[data-node]')).toHaveCount(3);
  await expect(map.getByText('default', { exact: true })).toBeVisible();
  await page.getByRole('combobox', { name: 'Group by' }).selectOption('zone');
  await expect(map.getByText('z1', { exact: true })).toBeVisible();
  await expect(map.getByText('default', { exact: true })).toHaveCount(0);
});
