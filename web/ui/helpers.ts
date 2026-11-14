import type { Page, Route } from '@playwright/test';

export const me = {
  user: { id: 'u1', provider: 'google', email: 'ada@example.com', name: 'Ada' },
  csrfToken: 'csrf-1',
  settings: { favouriteClusters: [], watchIdleSeconds: 30, theme: 'system' },
  maintenanceBanner: '',
  features: {
    kubeconfig: true,
    fileBrowser: true,
    nodeShell: false,
    vault: true,
    helmVersion: 4,
    modules: {},
  },
  limits: {
    fileTransferBytes: 1,
    logsHistoryMaxLines: 2,
    logsLiveBufferLines: 3,
    vaultExpiryWarningSeconds: 1800,
  },
  vault: null,
  keyId: 'k1',
};

export const json = (body: unknown, status = 200) => ({
  status,
  contentType: 'application/json',
  body: JSON.stringify(body),
});

export const unauthenticated = json({ error: { code: 'unauthenticated', message: 'Sign in.' } }, 401);

type Handler = (route: Route) => Promise<void> | void;

/** Answers API requests from a table of "METHOD /path" → handler. Anything else is a 404 that fails the test. */
export async function mockApi(page: Page, routes: Record<string, Handler>): Promise<string[]> {
  const unhandled: string[] = [];
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url());
    const key = `${route.request().method()} ${url.pathname.replace('/api/v1', '')}`;
    const handler = routes[key];
    if (!handler) {
      unhandled.push(key);
      await route.fulfill(json({ error: { code: 'not_found', message: key } }, 404));
      return;
    }
    await handler(route);
  });
  return unhandled;
}
