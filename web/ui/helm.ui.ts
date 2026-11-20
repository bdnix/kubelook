import { expect, test } from '@playwright/test';
import { cluster, setup } from './fixtures';
import { installGuard } from './guard';
import { editorText, json } from './helpers';

installGuard();

const base = `/clusters/${cluster.path}/helm`;
const release = {
  name: 'web',
  namespace: 'shop',
  revision: 2,
  status: 'deployed',
  chart: 'web',
  chartVersion: '1.1.0',
  appVersion: '2.1',
  updated: new Date(Date.now() - 3600_000).toISOString(),
  applyMethod: 'csa',
};
const detail = {
  ...release,
  manifest: 'kind: ConfigMap\nmetadata:\n  name: web-config\n',
  notes: 'Thank you for installing web.\n',
  hooks: [{ name: 'web-test', kind: 'Pod', events: ['test'] }],
  history: [
    {
      revision: 2,
      status: 'deployed',
      chartVersion: '1.1.0',
      description: 'Upgrade complete',
      updated: release.updated,
    },
    {
      revision: 1,
      status: 'superseded',
      chartVersion: '1.0.0',
      description: 'Install complete',
      updated: release.updated,
    },
  ],
  valuesHidden: true,
};

const job = (over: Record<string, unknown> = {}) => ({
  id: 'j1',
  cluster: 'v:prod',
  type: 'helm-upgrade',
  target: { name: 'web', namespace: 'shop', action: 'upgrade', startRevision: 2 },
  status: 'running',
  progress: { done: 0, total: 0, text: 'Running helm upgrade for shop/web' },
  message: '',
  offers: [],
  createdAt: '2026-11-20T00:00:00Z',
  updatedAt: '2026-11-20T00:00:01Z',
  ...over,
});

function routes(state: { revealed: number; submitted?: unknown }) {
  return {
    [`GET ${base}/releases`]: (r: import('@playwright/test').Route) =>
      r.fulfill(json({ releases: [release] })),
    [`GET ${base}/releases/shop/web`]: (r: import('@playwright/test').Route) => r.fulfill(json(detail)),
    [`POST ${base}/releases/shop/web/values/reveal`]: (r: import('@playwright/test').Route) => {
      state.revealed++;
      return r.fulfill(
        json({ userValues: 'replicas: 3\n', computedValues: 'replicas: 3\nmessage: hello\n' }),
      );
    },
    [`GET ${base}/repositories`]: (r: import('@playwright/test').Route) =>
      r.fulfill(
        json({
          repositories: [{ name: 'charts', url: 'https://charts.example.org', scope: 'org', hasAuth: false }],
        }),
      ),
    [`POST ${base}/preview`]: (r: import('@playwright/test').Route) =>
      r.fulfill(json({ currentManifest: 'replicas: "3"\n', newManifest: 'replicas: "5"\n' })),
    [`POST ${base}/jobs`]: (r: import('@playwright/test').Route) => {
      state.submitted = r.request().postDataJSON();
      return r.fulfill(json({ jobId: 'j1' }));
    },
    'GET /jobs': (r: import('@playwright/test').Route) =>
      r.fulfill(json({ jobs: state.submitted ? [job()] : [] })),
  };
}

test('the releases list, with the values hidden until Reveal', async ({ page }) => {
  const state = { revealed: 0 };
  await setup(page, routes(state));
  await page.goto(`/c/${cluster.path}/helm`);

  await expect(page.getByText(/This console uses Helm 4/)).toBeVisible();
  const table = page.getByRole('table', { name: 'Releases' });
  const row = table.getByRole('row', { name: /web/ });
  await expect(row).toContainText('shop');
  await expect(row).toContainText('deployed');
  await expect(row).toContainText('web 1.1.0');

  await row.getByRole('button', { name: 'web' }).click();
  const details = page.getByRole('region', { name: 'Release web' });
  await expect(details.getByText('replicas: 3')).toHaveCount(0);
  expect(state.revealed).toBe(0);
  await details.getByRole('button', { name: 'Reveal values' }).click();
  await expect(details.getByText(/message: hello/)).toBeVisible();
  expect(state.revealed).toBe(1);
  await details.getByRole('button', { name: 'Hide values' }).click();
  await expect(details.getByText(/message: hello/)).toHaveCount(0);

  await details.getByRole('tab', { name: 'Notes' }).click();
  await expect(details.getByText('Thank you for installing web.')).toBeVisible();
});

test('the upgrade dialog shows both diffs before Confirm is enabled, and a submitted job shows its progress row', async ({
  page,
}) => {
  const state: { revealed: number; submitted?: unknown } = { revealed: 0 };
  await setup(page, routes(state));
  await page.goto(`/c/${cluster.path}/helm`);
  await page.getByRole('button', { name: 'web', exact: true }).click();
  await page.getByRole('region', { name: 'Release web' }).getByRole('button', { name: 'Upgrade' }).click();

  const dialog = page.getByRole('dialog', { name: 'Upgrade web' });
  await expect.poll(() => editorText(page)).toContain('replicas: 3');
  await expect(dialog.getByRole('button', { name: 'Confirm upgrade' })).toBeDisabled();
  await dialog.getByRole('combobox', { name: 'Repository' }).selectOption('charts');
  await page.locator('.monaco-editor').first().click();
  await page.keyboard.press('Control+End');
  await page.keyboard.insertText('\nmessage: hi');
  await dialog.getByRole('button', { name: 'Preview' }).click();
  await expect(dialog.getByRole('region', { name: 'Values diff' })).toContainText('message: hi');
  await expect(dialog.getByRole('region', { name: 'Manifest diff' })).toContainText('replicas: "5"');
  await expect(dialog.getByRole('button', { name: 'Confirm upgrade' })).toBeEnabled();

  await dialog.getByRole('button', { name: 'Confirm upgrade' }).click();
  await expect.poll(() => (state.submitted as { action?: string } | undefined)?.action).toBe('upgrade');
  const jobs = page.getByRole('region', { name: 'Helm jobs' });
  await expect(jobs).toContainText('Running');
});
