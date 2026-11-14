import 'fake-indexeddb/auto';
import { screen, within } from '@testing-library/react';
import { IDBFactory } from 'fake-indexeddb';
import { beforeEach, describe, expect, it } from 'vitest';
import { vaultStatusStore } from '../../state/vaultStatus';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { openVaultStore } from '../credentials/vaultStore';
import { KEY_CHANGED_NOTICE } from '../credentials/useRestore';
import { AppRoutes } from './routes';

function me(over: Record<string, unknown> = {}) {
  return {
    user: { id: 'u1', provider: 'google', email: 'a@example.com', name: 'Ann' },
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
    keyId: 'k',
    ...over,
  };
}

const oneCluster = [
  {
    type: 'vault',
    label: 'Vault',
    status: 'ok',
    clusters: [{ id: 'v:prod', name: 'prod', roles: ['r'], state: 'closed' }],
  },
];

function signedIn(
  opts: { sources?: unknown[]; me?: Record<string, unknown>; extra?: Record<string, () => Response> } = {},
) {
  return fakeClient({
    'GET /me': () => jsonResponse(me(opts.me)),
    'GET /clusters': () => jsonResponse({ sources: opts.sources ?? oneCluster }),
    ...opts.extra,
  });
}
const signedOut = fakeClient({
  'GET /me': () => jsonResponse({ error: { code: 'unauthenticated', message: 'Sign in.' } }, 401),
  'GET /auth/providers': () => jsonResponse({ providers: ['google'] }),
});

beforeEach(() => {
  globalThis.indexedDB = new IDBFactory();
  vaultStatusStore.getState().clear();
});

describe('AppRoutes (signed in)', () => {
  it('/ with clusters shows the cluster picker', async () => {
    renderApp(<AppRoutes />, { client: signedIn() });
    expect(await screen.findByRole('heading', { name: 'Choose a cluster' })).toBeInTheDocument();
  });

  it('/ without any credentials is the landing page with both forms', async () => {
    renderApp(<AppRoutes />, { client: signedIn({ sources: [] }) });
    expect(await screen.findByRole('heading', { name: 'Add your credentials' })).toBeInTheDocument();
    expect(screen.getByLabelText('Vault token')).toBeInTheDocument();
    expect(screen.getByLabelText('Kubeconfig')).toBeInTheDocument();
  });

  it('the landing page hides the forms the server does not offer', async () => {
    const features = {
      kubeconfig: false,
      fileBrowser: true,
      nodeShell: false,
      vault: false,
      helmVersion: 4,
      modules: {},
    };
    renderApp(<AppRoutes />, { client: signedIn({ sources: [], me: { features } }) });
    expect(
      await screen.findByText('This server accepts no credentials. Ask an administrator.'),
    ).toBeInTheDocument();
  });

  it('treats a failing cluster list like an empty one', async () => {
    const client = signedIn({
      extra: { 'GET /clusters': () => jsonResponse({ error: { code: 'internal', message: 'x' } }, 500) },
    });
    renderApp(<AppRoutes />, { client });
    expect(await screen.findByRole('heading', { name: 'Add your credentials' })).toBeInTheDocument();
  });

  it('shows the clusters in the rail, favourites first', async () => {
    const sources = [
      {
        type: 'vault',
        label: 'Vault',
        status: 'ok',
        clusters: [
          { id: 'v:a', name: 'a', state: 'closed' },
          { id: 'v:b', name: 'b', state: 'closed' },
        ],
      },
    ];
    const settings = { favouriteClusters: ['v:b'], watchIdleSeconds: 30, theme: 'system' };
    renderApp(<AppRoutes />, { client: signedIn({ sources, me: { settings } }) });
    const rail = await screen.findByRole('navigation', { name: 'Clusters' });
    expect(await within(rail).findByRole('heading', { name: 'Favourites' })).toBeInTheDocument();
    expect(
      within(rail)
        .getAllByRole('link')
        .map((l) => l.textContent),
    ).toEqual(['b', 'a']);
  });

  it('/settings shows the settings with the forms and the saved credentials', async () => {
    renderApp(<AppRoutes />, { client: signedIn(), route: '/settings' });
    expect(await screen.findByRole('heading', { name: 'Settings' })).toBeInTheDocument();
    expect(await screen.findByLabelText('Vault token')).toBeInTheDocument();
    expect(await screen.findByRole('heading', { name: 'Saved credentials' })).toBeInTheDocument();
  });

  it('/c/:cluster/* shows the decoded cluster ID', async () => {
    renderApp(<AppRoutes />, { client: signedIn(), route: '/c/k%3Aprod%2Fctx/ns/default' });
    expect(await screen.findByText('k:prod/ctx')).toBeInTheDocument();
  });

  it('unknown paths show "Page not found"', async () => {
    renderApp(<AppRoutes />, { client: signedIn(), route: '/nope/never' });
    expect(await screen.findByText('Page not found')).toBeInTheDocument();
  });

  it('shows the maintenance banner', async () => {
    renderApp(<AppRoutes />, { client: signedIn({ me: { maintenanceBanner: 'Back at noon' } }) });
    expect(await screen.findByText('Back at noon')).toBeInTheDocument();
  });

  it('restores saved copies after sign-in and tells the user when the key changed', async () => {
    await openVaultStore('u1').put({
      id: 'vault_token',
      kind: 'vault_token',
      label: 'ada',
      sealed: 'S1',
      savedAt: '2026-11-14T12:00:00Z',
    });
    const client = signedIn({
      extra: {
        'POST /credentials/restore': () =>
          jsonResponse({ results: [{ status: 'key_changed', message: 'x' }] }),
      },
    });
    renderApp(<AppRoutes />, { client });
    expect(await screen.findByText(KEY_CHANGED_NOTICE)).toBeInTheDocument();
    expect(await openVaultStore('u1').list()).toEqual([]);
  });
});

describe('AppRoutes (signed out)', () => {
  it.each(['/', '/settings', '/c/v%3Aprod', '/nope'])('%s shows the sign-in page', async (route) => {
    renderApp(<AppRoutes />, { client: signedOut, route });
    expect(await screen.findByRole('heading', { level: 1, name: 'Kubelook' })).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: 'Sign in with Google' })).toBeInTheDocument();
    expect(screen.queryByRole('navigation', { name: 'Clusters' })).toBeNull();
  });
});

describe('AppRoutes (loading and failure)', () => {
  it('shows a loading status first', () => {
    renderApp(<AppRoutes />, { client: signedIn() });
    expect(screen.getByRole('status')).toHaveTextContent('Loading');
  });

  it('shows an error banner when the server cannot be reached', async () => {
    const broken = fakeClient({
      'GET /me': () => jsonResponse({ error: { code: 'internal', message: 'x' } }, 500),
    });
    renderApp(<AppRoutes />, { client: broken });
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not reach the server');
  });
});
