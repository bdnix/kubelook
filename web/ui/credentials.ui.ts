import { expect, test } from '@playwright/test';
import { installGuard } from './guard';
import { json, me, mockApi } from './helpers';

installGuard();

const kubeconfig = 'apiVersion: v1\nkind: Config\n';
const source = {
  type: 'kubeconfig',
  label: 'work',
  status: 'ok',
  clusters: [{ id: 'k:work/prod', name: 'prod', state: 'closed', server: 'https://api.example:6443' }],
};

test('adding a kubeconfig shows its clusters, saves a copy in this browser and restores it after a reload', async ({
  page,
}) => {
  let added = false;
  const restoreBodies: unknown[] = [];
  await mockApi(page, {
    'GET /me': (r) => r.fulfill(json(me)),
    'GET /clusters': (r) => r.fulfill(json({ sources: added ? [source] : [] })),
    'POST /credentials/kubeconfig': (r) => {
      expect(r.request().headers()['x-csrf-token']).toBe('csrf-1');
      expect(r.request().postDataJSON()).toEqual({ label: 'work', content: kubeconfig });
      added = true;
      return r.fulfill(json({ sealed: 'v1.k.sealed.copy', label: 'work', clusters: [] }));
    },
    'POST /clusters/k%3Awork%2Fprod/open': (r) => r.fulfill(json({ state: 'open' })),
    'POST /credentials/restore': (r) => {
      restoreBodies.push(r.request().postDataJSON());
      return r.fulfill(json({ results: [{ status: 'ok', kind: 'kubeconfig', label: 'work' }] }));
    },
  });

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Add your credentials' })).toBeVisible();

  await page.getByLabel('Label').fill('work');
  await page.getByLabel('Kubeconfig', { exact: true }).fill(kubeconfig);
  await page.getByRole('button', { name: 'Add kubeconfig' }).click();

  const rail = page.getByRole('navigation', { name: 'Clusters' });
  await expect(rail.getByRole('heading', { name: 'work' })).toBeVisible();
  await expect(rail.getByRole('link', { name: 'prod' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Choose a cluster' })).toBeVisible();

  await rail.getByRole('link', { name: 'prod' }).click();
  await expect(page).toHaveURL(/\/c\/k(:|%3A)work%2Fprod$/);

  await page.goto('/settings');
  await expect(page.getByRole('heading', { name: 'Saved credentials' })).toBeVisible();
  await expect(page.getByText('Kubeconfig', { exact: true }).last()).toBeVisible();

  // Every full page load keeps the encrypted copy in IndexedDB and sends it back to the server.
  await page.reload();
  await expect(rail.getByRole('link', { name: 'prod' })).toBeVisible();
  // Each full page load (/settings, then the reload) sends the saved copy; only the content matters.
  await expect.poll(() => restoreBodies.length).toBeGreaterThanOrEqual(2);
  for (const body of restoreBodies) expect(body).toEqual({ items: [{ sealed: 'v1.k.sealed.copy' }] });
});

test('a re-sealed copy prepared by the server replaces the saved one, so the next sign-in restores the new copy', async ({
  page,
}) => {
  let acked = false;
  const restoreBodies: unknown[] = [];
  const ackBodies: unknown[] = [];
  await mockApi(page, {
    'GET /me': (r) => r.fulfill(json(me)),
    'GET /clusters': (r) => r.fulfill(json({ sources: [source] })),
    'POST /clusters/k%3Awork%2Fprod/open': (r) => r.fulfill(json({ state: 'open' })),
    'GET /credentials/pending': (r) =>
      r.fulfill(json({ items: acked ? [] : [{ label: 'work', sealed: 'v1.k.sealed.rotated' }] })),
    'POST /credentials/pending/ack': (r) => {
      ackBodies.push(r.request().postDataJSON());
      acked = true;
      return r.fulfill(json({}));
    },
    'POST /credentials/restore': (r) => {
      restoreBodies.push(r.request().postDataJSON());
      return r.fulfill(json({ results: [{ status: 'ok', kind: 'kubeconfig', label: 'work' }] }));
    },
  });

  await page.goto('/');
  // Seed the saved copy the way adding the kubeconfig does, then reload so it is restored.
  await page.evaluate(async () => {
    const db: IDBDatabase = await new Promise((resolve, reject) => {
      const req = indexedDB.open('kubelook', 1);
      req.onupgradeneeded = () => {
        const store = req.result.createObjectStore('credentials', { keyPath: 'key' });
        store.createIndex('userId', 'userId');
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction('credentials', 'readwrite');
      tx.objectStore('credentials').put({
        key: 'u1/kubeconfig:work',
        userId: 'u1',
        id: 'kubeconfig:work',
        kind: 'kubeconfig',
        label: 'work',
        sealed: 'v1.k.sealed.original',
        savedAt: '2026-11-01T00:00:00Z',
      });
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
    db.close();
  });
  await page.reload();
  await expect.poll(() => restoreBodies.length).toBeGreaterThanOrEqual(1);

  // Opening the cluster fetches the pending copy, stores it and acknowledges it.
  const rail = page.getByRole('navigation', { name: 'Clusters' });
  await rail.getByRole('link', { name: 'prod' }).click();
  await expect.poll(() => ackBodies).toEqual([{ labels: ['work'] }]);

  // The next sign-in sends the new copy.
  await page.reload();
  await expect
    .poll(() => JSON.stringify(restoreBodies.at(-1)))
    .toBe(JSON.stringify({ items: [{ sealed: 'v1.k.sealed.rotated' }] }));
});

test('a server that rejects the kubeconfig has its message shown as text', async ({ page }) => {
  const message = 'kubeconfig user "eks-user": exec credential plugins are not supported';
  await mockApi(page, {
    'GET /me': (r) => r.fulfill(json(me)),
    'GET /clusters': (r) => r.fulfill(json({ sources: [] })),
    'POST /credentials/kubeconfig': (r) => r.fulfill(json({ error: { code: 'bad_request', message } }, 400)),
  });
  await page.goto('/');
  await page.getByLabel('Label').fill('work');
  await page.getByLabel('Kubeconfig', { exact: true }).fill('x: y');
  await page.getByRole('button', { name: 'Add kubeconfig' }).click();
  await expect(page.getByRole('alert')).toHaveText(message);
});

test('after a key change the saved copies are dropped and the user is told', async ({ page }) => {
  let restored = false;
  await mockApi(page, {
    'GET /me': (r) => r.fulfill(json(me)),
    'GET /clusters': (r) => r.fulfill(json({ sources: [] })),
    'POST /credentials/kubeconfig': (r) =>
      r.fulfill(json({ sealed: 'old.copy', label: 'work', clusters: [] })),
    'POST /credentials/restore': (r) => {
      restored = true;
      return r.fulfill(json({ results: [{ status: 'key_changed' }] }));
    },
  });
  await page.goto('/');
  await page.getByLabel('Label').fill('work');
  await page.getByLabel('Kubeconfig', { exact: true }).fill('x: y');
  await page.getByRole('button', { name: 'Add kubeconfig' }).click();
  await expect.poll(() => page.evaluate(async () => (await indexedDB.databases()).length)).toBe(1);

  await page.reload();
  await expect(page.getByText(/encryption key was changed/)).toBeVisible();
  expect(restored).toBe(true);
  await page.goto('/settings');
  await expect(page.getByText('No credentials are saved in this browser.')).toBeVisible();
});

test('unknown addresses say so inside the app shell', async ({ page }) => {
  await mockApi(page, {
    'GET /me': (r) => r.fulfill(json(me)),
    'GET /clusters': (r) => r.fulfill(json({ sources: [] })),
  });
  await page.goto('/nope/never');
  await expect(page.getByText('Page not found')).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Clusters' })).toBeVisible();
});
