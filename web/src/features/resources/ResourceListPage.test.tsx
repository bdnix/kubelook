import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { ClusterPage } from '../clusters/ClusterPage';
import { LiveContext } from '../../state/useLive';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { makeLiveStore } from '../../test/live';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { Route, Routes, useLocation } from 'react-router';
import { ResourceListPage } from './ResourceListPage';

const clusters = (state: string) => ({
  sources: [
    { type: 'vault', label: 'Vault', status: 'ok', clusters: [{ id: 'v:prod', name: 'prod', state }] },
  ],
});
const discovery = {
  serverVersion: 'v1.35.0',
  resources: [
    {
      group: '',
      version: 'v1',
      resource: 'pods',
      kind: 'Pod',
      namespaced: true,
      verbs: ['list'],
      preferred: true,
      servedVersions: ['v1'],
    },
  ],
};
const pod = (uid: string, name: string, phase = 'Running') => ({
  metadata: { uid, name, namespace: 'ns', creationTimestamp: '2026-11-14T10:00:00Z' },
  spec: { nodeName: 'node-a', containers: [{ name: 'a' }] },
  status: {
    phase,
    containerStatuses: [{ ready: phase === 'Running', restartCount: 0, state: { running: {} } }],
  },
});

function mount(state = 'open', extra: Record<string, () => Response> = {}) {
  const store = makeLiveStore();
  const client = fakeClient({
    'GET /clusters': () => jsonResponse(clusters(state)),
    'POST /clusters/v%3Aprod/open': () => jsonResponse({ state: 'open' }),
    'GET /clusters/v%3Aprod/discovery': () => jsonResponse(discovery),
    ...extra,
  });
  renderApp(
    <LiveContext.Provider value={store}>
      <ResourceListPage cluster="v:prod" group="core" kind="Pod" />
    </LiveContext.Provider>,
    { client },
  );
}

async function connected() {
  await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
  act(() => FakeWebSocket.last.serverOpen());
}

describe('ResourceListPage', () => {
  it('subscribes to the live list of an open cluster and shows the pods', async () => {
    mount();
    await connected();
    expect(FakeWebSocket.last.frames).toContainEqual({
      op: 'watch.open',
      ch: 1,
      d: { cluster: 'v:prod', group: 'core', version: 'v1', resource: 'pods', namespace: '' },
    });
    expect(screen.getByRole('status')).toHaveTextContent('Loading');
    act(() =>
      FakeWebSocket.last.serverSend({
        ch: 1,
        t: 'snapshot',
        d: { items: [pod('1', 'web-1'), pod('2', 'web-2', 'Pending')], more: false },
      }),
    );
    expect(await screen.findByRole('heading', { name: 'Pods' })).toBeInTheDocument();
    const grid = screen.getByRole('grid', { name: 'Pods' });
    expect(within(grid).getByText('web-1')).toBeInTheDocument();
    expect(within(grid).getByText('web-2')).toBeInTheDocument();
    expect(within(grid).getByRole('img', { name: 'healthy' })).toBeInTheDocument();
    expect(within(grid).getByRole('img', { name: 'needs attention' })).toBeInTheDocument();

    act(() =>
      FakeWebSocket.last.serverSend({ ch: 1, t: 'event', d: { type: 'ADDED', object: pod('3', 'web-3') } }),
    );
    expect(await within(grid).findByText('web-3')).toBeInTheDocument();
  });

  it('opens a cluster that is not open before listing', async () => {
    mount('closed', { 'GET /clusters/v%3Aprod/discovery': () => jsonResponse(discovery) });
    expect(screen.getByRole('status')).toHaveTextContent('Opening the cluster');
    await connected();
  });

  it('says so when the user may not list the kind', async () => {
    mount();
    await connected();
    act(() =>
      FakeWebSocket.last.serverSend({ ch: 1, t: 'error', d: { code: 'forbidden', message: 'RBAC' } }),
    );
    expect(await screen.findByText("You don't have permission to list Pods.")).toBeInTheDocument();
  });

  it('shows other errors as text', async () => {
    mount();
    await connected();
    act(() =>
      FakeWebSocket.last.serverSend({
        ch: 1,
        t: 'error',
        d: { code: 'internal', message: 'the watch failed' },
      }),
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('the watch failed');
  });

  it('shows that live updates are paused and keeps the rows', async () => {
    mount();
    await connected();
    act(() =>
      FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { items: [pod('1', 'web-1')], more: false } }),
    );
    await screen.findByText('web-1');
    act(() => FakeWebSocket.last.serverSend({ ch: 1, t: 'paused', d: {} }));
    expect(await screen.findByText(/Live updates paused/)).toBeInTheDocument();
    expect(screen.getByText('web-1')).toBeInTheDocument();
  });

  it('reports a failed open and a kind the cluster does not serve', async () => {
    mount('closed', {
      'POST /clusters/v%3Aprod/open': () =>
        jsonResponse({ error: { code: 'unprocessable', message: 'no API server configured' } }, 422),
    });
    expect(await screen.findByRole('alert')).toHaveTextContent('no API server configured');
  });

  it('says when the cluster does not serve the kind, and when discovery fails', async () => {
    const store = makeLiveStore();
    const base = { 'GET /clusters': () => jsonResponse(clusters('open')) };
    const { unmount } = renderApp(
      <LiveContext.Provider value={store}>
        <ResourceListPage cluster="v:prod" group="example.io" kind="Widget" />
      </LiveContext.Provider>,
      { client: fakeClient({ ...base, 'GET /clusters/v%3Aprod/discovery': () => jsonResponse(discovery) }) },
    );
    expect(await screen.findByText('This cluster does not serve Widget.')).toBeInTheDocument();
    unmount();
    renderApp(
      <LiveContext.Provider value={store}>
        <ResourceListPage cluster="v:prod" group="core" kind="Pod" />
      </LiveContext.Provider>,
      {
        client: fakeClient({
          ...base,
          'GET /clusters/v%3Aprod/discovery': () =>
            jsonResponse({ error: { code: 'internal', message: 'discovery broke' } }, 500),
        }),
      },
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('discovery broke');
  });
});

describe('ClusterPage', () => {
  it('sends the cluster URL to the Pods list and routes other kinds', async () => {
    const store = makeLiveStore();
    const client = fakeClient({
      'GET /clusters': () => jsonResponse(clusters('open')),
      'GET /clusters/v%3Aprod/discovery': () => jsonResponse(discovery),
    });
    renderApp(
      <LiveContext.Provider value={store}>
        <Routes>
          <Route path="/c/:cluster/*" element={<ClusterPage />} />
        </Routes>
      </LiveContext.Provider>,
      { client, route: '/c/v%3Aprod' },
    );
    await connected();
    expect(FakeWebSocket.last.frames).toContainEqual(expect.objectContaining({ op: 'watch.open' }));
  });

  it('keeps a "/" in the cluster ID encoded when it redirects to the Pods list', async () => {
    const store = makeLiveStore();
    const opened: string[] = [];
    const client = fakeClient({
      'GET /clusters': () =>
        jsonResponse({
          sources: [
            {
              type: 'kubeconfig',
              label: 'work',
              status: 'ok',
              clusters: [{ id: 'k:work/prod', name: 'prod', state: 'closed' }],
            },
          ],
        }),
      'POST /clusters/k%3Awork%2Fprod/open': () => {
        opened.push('open');
        return jsonResponse({ state: 'open' });
      },
      'GET /clusters/k%3Awork%2Fprod/discovery': () => jsonResponse(discovery),
    });
    renderApp(
      <LiveContext.Provider value={store}>
        <Routes>
          <Route path="/c/:cluster/*" element={<ClusterPage />} />
        </Routes>
      </LiveContext.Provider>,
      { client, route: '/c/k%3Awork%2Fprod' },
    );
    await connected();
    expect(opened).toEqual(['open']);
    expect(FakeWebSocket.last.frames).toContainEqual(
      expect.objectContaining({ op: 'watch.open', d: expect.objectContaining({ cluster: 'k:work/prod' }) }),
    );
  });

  it('shows not found for unknown sub-pages', () => {
    renderApp(
      <Routes>
        <Route path="/c/:cluster/*" element={<ClusterPage />} />
      </Routes>,
      { route: '/c/v%3Aprod/a/b/c/d' },
    );
    expect(screen.getByText('Page not found')).toBeInTheDocument();
  });
});

vi.mock('../../adapters/monaco/YamlEditor', () => ({
  YamlEditor: (p: { value: string; label: string }) => (
    <textarea aria-label={p.label} value={p.value} readOnly />
  ),
}));

describe('ResourceListPage: create from YAML', () => {
  it('toggles the create panel above the list', async () => {
    mount();
    await connected();
    act(() => FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { items: [], more: false } }));
    await userEvent.click(await screen.findByRole('button', { name: 'Create from YAML' }));
    expect(screen.getByRole('region', { name: 'Create from YAML' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(screen.queryByRole('region', { name: 'Create from YAML' })).toBeNull();
  });
});

describe('ResourceListPage: opening a row', () => {
  function Where() {
    return <p data-testid="where">{useLocation().pathname}</p>;
  }

  it('navigates to the deep link of the object', async () => {
    const store = makeLiveStore();
    const client = fakeClient({
      'GET /clusters': () => jsonResponse(clusters('open')),
      'GET /clusters/v%3Aprod/discovery': () => jsonResponse(discovery),
    });
    renderApp(
      <LiveContext.Provider value={store}>
        <ResourceListPage cluster="v:prod" group="core" kind="Pod" />
        <Where />
      </LiveContext.Provider>,
      { client },
    );
    await connected();
    act(() =>
      FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { items: [pod('1', 'web-1')], more: false } }),
    );
    const row = (await screen.findByText('web-1')).closest('[role="row"]') as HTMLElement;
    row.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
    await waitFor(() =>
      expect(screen.getByTestId('where')).toHaveTextContent('/c/v%3Aprod/ns/ns/core/Pod/web-1'),
    );
  });
});
