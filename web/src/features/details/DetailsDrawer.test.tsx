import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes, useLocation } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { dockStore } from '../../state/dock';
import { LiveContext } from '../../state/useLive';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { makeLiveStore } from '../../test/live';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { DetailsDrawer } from './DetailsDrawer';
import { DrawerRoute } from './DrawerRoute';

const res = (group: string, version: string, resource: string, kind: string, namespaced = true) => ({
  group,
  version,
  resource,
  kind,
  namespaced,
  verbs: ['list'],
  preferred: true,
  servedVersions: [version],
});
const discovery = {
  serverVersion: 'v1.35.0',
  resources: [
    res('', 'v1', 'pods', 'Pod'),
    res('', 'v1', 'nodes', 'Node', false),
    res('apps', 'v1', 'deployments', 'Deployment'),
    res('apps', 'v1', 'replicasets', 'ReplicaSet'),
    res('batch', 'v1', 'jobs', 'Job'),
    res('events.k8s.io', 'v1', 'events', 'Event'),
  ],
};

const pod = {
  apiVersion: 'v1',
  kind: 'Pod',
  metadata: {
    uid: 'pod-1',
    name: 'web-1',
    namespace: 'ns',
    creationTimestamp: '2026-11-14T10:00:00Z',
    ownerReferences: [
      { apiVersion: 'apps/v1', kind: 'ReplicaSet', name: 'web-abc', uid: 'rs-1', controller: true },
    ],
  },
  spec: {
    containers: [
      { name: 'app', image: 'nginx' },
      { name: 'side', image: 'busybox' },
    ],
  },
  status: {
    phase: 'Running',
    containerStatuses: [{ name: 'app', ready: true, restartCount: 0, state: { running: {} } }],
  },
};

function client(extra: Record<string, () => Response> = {}) {
  return fakeClient({
    'GET /clusters/v%3Aprod/discovery': () => jsonResponse(discovery),
    'GET /clusters/v%3Aprod/resources/apps/v1/replicasets/web-abc?namespace=ns': () =>
      jsonResponse({
        apiVersion: 'apps/v1',
        kind: 'ReplicaSet',
        metadata: {
          uid: 'rs-1',
          name: 'web-abc',
          namespace: 'ns',
          ownerReferences: [
            { apiVersion: 'apps/v1', kind: 'Deployment', name: 'web', uid: 'dep-1', controller: true },
          ],
        },
      }),
    'GET /clusters/v%3Aprod/resources/apps/v1/deployments/web?namespace=ns': () =>
      jsonResponse({
        apiVersion: 'apps/v1',
        kind: 'Deployment',
        metadata: { uid: 'dep-1', name: 'web', namespace: 'ns' },
      }),
    ...extra,
  });
}

function Where() {
  return <p data-testid="where">{useLocation().pathname}</p>;
}

type Props = Partial<React.ComponentProps<typeof DetailsDrawer>>;

function mount(props: Props = {}) {
  const store = makeLiveStore();
  const onClose = vi.fn();
  const view = renderApp(
    <LiveContext.Provider value={store}>
      <button type="button">row</button>
      <DetailsDrawer
        cluster="v:prod"
        group=""
        kind="Pod"
        namespace="ns"
        name="web-1"
        onClose={onClose}
        {...props}
      />
    </LiveContext.Provider>,
    { client: client() },
  );
  return { onClose, ...view };
}

async function connected() {
  await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
  act(() => FakeWebSocket.last.serverOpen());
}

function snapshot(ch: number, items: unknown[]) {
  act(() => FakeWebSocket.last.serverSend({ ch, t: 'snapshot', d: { items, more: false } }));
}

beforeEach(() => dockStore.setState({ tabs: [], active: null }));

describe('DetailsDrawer', () => {
  it('watches the one object by name and shows it with its tabs and status', async () => {
    mount();
    await connected();
    expect(FakeWebSocket.last.frames).toContainEqual({
      op: 'watch.open',
      ch: 1,
      d: {
        cluster: 'v:prod',
        group: 'core',
        version: 'v1',
        resource: 'pods',
        namespace: 'ns',
        fieldSelector: 'metadata.name=web-1',
      },
    });
    expect(screen.getByRole('status')).toHaveTextContent('Loading');
    snapshot(1, [pod]);
    expect(await screen.findByRole('heading', { name: 'web-1' })).toBeInTheDocument();
    expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual([
      'Overview',
      'YAML',
      'Events',
      'Logs',
      'Files',
      'Related',
    ]);
    expect(screen.getByRole('img', { name: 'healthy' })).toBeInTheDocument();
    expect(screen.getByRole('table', { name: 'Containers' })).toBeInTheDocument();
  });

  it('follows the owner chain with plain reads', async () => {
    mount();
    await connected();
    snapshot(1, [pod]);
    expect(await screen.findByRole('link', { name: 'ReplicaSet/web-abc' })).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: 'Deployment/web' })).toBeInTheDocument();
  });

  it('non-pod kinds have no Files tab, and workloads have Logs', async () => {
    mount({ group: 'apps', kind: 'Deployment', name: 'web' });
    await connected();
    snapshot(1, [
      {
        apiVersion: 'apps/v1',
        kind: 'Deployment',
        metadata: { uid: 'dep-1', name: 'web', namespace: 'ns' },
        spec: { selector: { matchLabels: { app: 'web' } } },
      },
    ]);
    await screen.findByRole('heading', { name: 'web' });
    expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual([
      'Overview',
      'YAML',
      'Events',
      'Logs',
      'Related',
    ]);
  });

  it('shows the YAML of the object, status included', async () => {
    mount();
    await connected();
    snapshot(1, [pod]);
    await userEvent.click(await screen.findByRole('tab', { name: 'YAML' }));
    const yaml = screen.getByLabelText('YAML');
    expect(yaml).toHaveTextContent('kind: Pod');
    expect(yaml).toHaveTextContent('phase: Running');
  });

  it('shows only the live events of the object, newest first', async () => {
    mount();
    await connected();
    snapshot(1, [pod]);
    await userEvent.click(await screen.findByRole('tab', { name: 'Events' }));
    await waitFor(() =>
      expect(
        FakeWebSocket.last.frames.some(
          (f) => (f.d as { resource?: string } | undefined)?.resource === 'events',
        ),
      ).toBe(true),
    );
    const ch =
      FakeWebSocket.last.frames.find((f) => (f.d as { resource?: string } | undefined)?.resource === 'events')
        ?.ch ?? 0;
    snapshot(ch, [
      {
        metadata: { uid: 'e1' },
        regarding: { uid: 'pod-1' },
        reason: 'Pulled',
        note: 'older',
        eventTime: '2026-11-14T10:00:00Z',
      },
      {
        metadata: { uid: 'e2' },
        regarding: { uid: 'pod-1' },
        reason: 'Started',
        note: 'newer',
        eventTime: '2026-11-14T11:00:00Z',
      },
      { metadata: { uid: 'e3' }, regarding: { uid: 'someone-else' }, reason: 'Other', note: 'not mine' },
    ]);
    const rows = within(await screen.findByRole('table', { name: 'Events' })).getAllByRole('row');
    expect(rows.map((r) => within(r).queryAllByRole('cell')[1]?.textContent).filter(Boolean)).toEqual([
      'Started',
      'Pulled',
    ]);
    expect(screen.queryByText('not mine')).toBeNull();
  });

  it('opens the logs of a pod in the dock', async () => {
    mount();
    await connected();
    snapshot(1, [pod]);
    await userEvent.click(await screen.findByRole('tab', { name: 'Logs' }));
    await userEvent.click(screen.getByRole('button', { name: 'Open logs for web-1' }));
    expect(dockStore.getState().tabs[0]).toMatchObject({
      title: 'Logs: web-1',
      props: { cluster: 'v:prod', namespace: 'ns', pod: 'web-1', containers: ['app', 'side'] },
    });
  });

  it('lists the pods of a workload on the Logs tab', async () => {
    mount({ group: 'apps', kind: 'Deployment', name: 'web' });
    await connected();
    snapshot(1, [
      {
        apiVersion: 'apps/v1',
        kind: 'Deployment',
        metadata: { uid: 'dep-1', name: 'web', namespace: 'ns' },
        spec: { selector: { matchLabels: { app: 'web' } } },
      },
    ]);
    await userEvent.click(await screen.findByRole('tab', { name: 'Logs' }));
    expect(screen.getByText('No pods to show logs for.')).toBeInTheDocument();
    const podsCh =
      FakeWebSocket.last.frames.find((f) => (f.d as { resource?: string } | undefined)?.resource === 'pods')
        ?.ch ?? 0;
    snapshot(podsCh, [
      { ...pod, metadata: { ...pod.metadata, labels: { app: 'web' } } },
      { ...pod, metadata: { uid: 'p2', name: 'other', namespace: 'ns', labels: { app: 'db' } } },
    ]);
    expect(await screen.findByRole('button', { name: 'Open logs for web-1' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Open logs for other' })).toBeNull();
  });

  it('shows a placeholder on the Files tab and the relations on the Related tab', async () => {
    mount();
    await connected();
    snapshot(1, [pod]);
    await userEvent.click(await screen.findByRole('tab', { name: 'Files' }));
    expect(screen.getByText(/file browser/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('tab', { name: 'Related' }));
    expect(
      await within(await screen.findByRole('region', { name: 'Owners' })).findByRole('link', {
        name: 'ReplicaSet/web-abc',
      }),
    ).toBeInTheDocument();
  });

  it('a Deployment lists its ReplicaSets as children', async () => {
    mount({ group: 'apps', kind: 'Deployment', name: 'web' });
    await connected();
    snapshot(1, [
      {
        apiVersion: 'apps/v1',
        kind: 'Deployment',
        metadata: { uid: 'dep-1', name: 'web', namespace: 'ns' },
        spec: {},
      },
    ]);
    await userEvent.click(await screen.findByRole('tab', { name: 'Related' }));
    const rsCh =
      FakeWebSocket.last.frames.find(
        (f) => (f.d as { resource?: string } | undefined)?.resource === 'replicasets',
      )?.ch ?? 0;
    snapshot(rsCh, [
      {
        apiVersion: 'apps/v1',
        kind: 'ReplicaSet',
        metadata: { uid: 'rs-1', name: 'web-abc', namespace: 'ns', ownerReferences: [{ uid: 'dep-1' }] },
      },
    ]);
    expect(
      await within(await screen.findByRole('region', { name: 'Children' })).findByRole('link', {
        name: 'ReplicaSet/web-abc',
      }),
    ).toBeInTheDocument();
  });

  it('closes on Escape and on the close button', async () => {
    const { onClose } = mount();
    await connected();
    snapshot(1, [pod]);
    await screen.findByRole('heading', { name: 'web-1' });
    await userEvent.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole('button', { name: 'Close details' }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('moves focus into the drawer and returns it to the row on close', async () => {
    const store = makeLiveStore();
    const { rerender } = renderApp(
      <LiveContext.Provider value={store}>
        <button type="button">row</button>
      </LiveContext.Provider>,
      { client: client() },
    );
    const row = screen.getByRole('button', { name: 'row' });
    row.focus();
    rerender(
      <LiveContext.Provider value={store}>
        <button type="button">row</button>
        <DetailsDrawer
          cluster="v:prod"
          group=""
          kind="Pod"
          namespace="ns"
          name="web-1"
          onClose={() => undefined}
        />
      </LiveContext.Provider>,
    );
    const drawer = screen.getByRole('region', { name: 'Pod web-1' });
    expect(drawer).toHaveFocus();
    rerender(
      <LiveContext.Provider value={store}>
        <button type="button">row</button>
      </LiveContext.Provider>,
    );
    expect(row).toHaveFocus();
  });

  it('says so, and disables nothing it does not have, when the object is deleted while open', async () => {
    mount();
    await connected();
    snapshot(1, [pod]);
    await screen.findByRole('heading', { name: 'web-1' });
    act(() => FakeWebSocket.last.serverSend({ ch: 1, t: 'event', d: { type: 'DELETED', object: pod } }));
    expect(await screen.findByText('This object was deleted.')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'web-1' })).toBeInTheDocument();
  });

  it('says when the object does not exist', async () => {
    mount();
    await connected();
    snapshot(1, []);
    expect(await screen.findByText('Pod web-1 was not found.')).toBeInTheDocument();
  });

  it('shows a watch error as text', async () => {
    mount();
    await connected();
    act(() =>
      FakeWebSocket.last.serverSend({
        ch: 1,
        t: 'error',
        d: { code: 'forbidden', message: 'pods is forbidden' },
      }),
    );
    expect(await screen.findByText('pods is forbidden')).toBeInTheDocument();
  });

  it('shows a discovery error', async () => {
    const store = makeLiveStore();
    renderApp(
      <LiveContext.Provider value={store}>
        <DetailsDrawer
          cluster="v:prod"
          group=""
          kind="Pod"
          namespace="ns"
          name="web-1"
          onClose={() => undefined}
        />
      </LiveContext.Provider>,
      {
        client: fakeClient({
          'GET /clusters/v%3Aprod/discovery': () =>
            jsonResponse({ error: { code: 'internal', message: 'discovery failed' } }, 500),
        }),
      },
    );
    expect(await screen.findByText('discovery failed')).toBeInTheDocument();
  });

  it('a kind the cluster does not serve is reported as not found', async () => {
    const store = makeLiveStore();
    renderApp(
      <LiveContext.Provider value={store}>
        <DetailsDrawer
          cluster="v:prod"
          group="example.io"
          kind="Widget"
          namespace="ns"
          name="w"
          onClose={() => undefined}
        />
      </LiveContext.Provider>,
      { client: client() },
    );
    expect(await screen.findByText('Widget w was not found.')).toBeInTheDocument();
  });
});

describe('DrawerRoute', () => {
  function app(route: string) {
    const store = makeLiveStore();
    return renderApp(
      <LiveContext.Provider value={store}>
        <Routes>
          <Route path="/c/:cluster/ns/:namespace/:group/:kind/:name" element={<DrawerRoute />} />
          <Route path="/c/:cluster/:group/:kind/:name" element={<DrawerRoute />} />
        </Routes>
        <Where />
      </LiveContext.Provider>,
      { client: client(), route },
    );
  }

  it('opens a namespaced object from its deep link, the empty group written as core', async () => {
    app('/c/v%3Aprod/ns/ns/core/Pod/web-1');
    await connected();
    expect(FakeWebSocket.last.frames).toContainEqual(
      expect.objectContaining({
        d: expect.objectContaining({
          resource: 'pods',
          namespace: 'ns',
          fieldSelector: 'metadata.name=web-1',
        }) as unknown,
      }),
    );
  });

  it('opens a cluster-scoped object without a namespace', async () => {
    app('/c/v%3Aprod/core/Node/n1');
    await connected();
    expect(FakeWebSocket.last.frames).toContainEqual(
      expect.objectContaining({
        d: expect.objectContaining({
          resource: 'nodes',
          namespace: '',
          fieldSelector: 'metadata.name=n1',
        }) as unknown,
      }),
    );
  });

  it('closing returns to the list of the kind', async () => {
    app('/c/v%3Aprod/ns/ns/apps/Deployment/web');
    await connected();
    await userEvent.keyboard('{Escape}');
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/c/v%3Aprod/apps/Deployment'));
  });
});
