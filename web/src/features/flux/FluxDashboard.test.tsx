import { act, screen, waitFor, within } from '@testing-library/react';
import { Route, Routes } from 'react-router';
import { describe, expect, it } from 'vitest';
import { findFluxKind } from '../../kinds/flux';
import { LiveContext } from '../../state/useLive';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { makeLiveStore } from '../../test/live';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { FluxDashboardView } from './FluxDashboard';
import { FluxPage } from './FluxPage';
import type { Controller, Summary } from './summary';

const def = (group: string, kind: string) => {
  const d = findFluxKind(group, kind);
  if (!d) throw new Error(kind);
  return d;
};
const summary = (over: Partial<Summary> = {}): Summary => ({
  total: 4,
  reportsStatus: true,
  ready: 2,
  failed: 1,
  inProgress: 1,
  suspended: 0,
  ...over,
});
const controllers: Controller[] = [
  { name: 'kustomize-controller', version: 'v2.7.0', ready: false, desired: 2, available: 1 },
  { name: 'source-controller', version: 'v2.7.0', ready: true, desired: 1, available: 1 },
];

describe('FluxDashboardView', () => {
  it('shows the counts per kind, with links to the lists', () => {
    renderApp(
      <FluxDashboardView
        cluster="c"
        rows={[
          {
            kind: {
              def: def('kustomize.toolkit.fluxcd.io', 'Kustomization'),
              group: 'kustomize.toolkit.fluxcd.io',
              version: 'v1',
              resource: 'kustomizations',
            },
            summary: summary(),
          },
          {
            kind: {
              def: def('notification.toolkit.fluxcd.io', 'Alert'),
              group: 'notification.toolkit.fluxcd.io',
              version: 'v1beta3',
              resource: 'alerts',
            },
            summary: summary({
              reportsStatus: false,
              total: 3,
              suspended: 1,
              ready: 0,
              failed: 0,
              inProgress: 0,
            }),
          },
        ]}
        controllers={controllers}
      />,
    );
    const table = screen.getByRole('table', { name: 'Flux objects' });
    const row = within(table).getByRole('row', { name: /Kustomization/ });
    expect(within(row).getByRole('link', { name: 'Kustomization' })).toHaveAttribute(
      'href',
      '/c/c/kustomize.toolkit.fluxcd.io/Kustomization',
    );
    expect(
      within(row)
        .getAllByRole('cell')
        .map((c) => c.textContent),
    ).toEqual(['Kustomization', '4', '2', '1', '1', '0']);
    const alert = within(table).getByRole('row', { name: /Alert/ });
    expect(
      within(alert)
        .getAllByRole('cell')
        .map((c) => c.textContent),
    ).toEqual(['Alert', '3', '–', '–', '–', '1']);
  });

  it('shows the Flux version and the health of each controller', () => {
    renderApp(<FluxDashboardView cluster="c" rows={[]} controllers={controllers} />);
    expect(screen.getByText(/Flux v2\.7\.0/)).toBeInTheDocument();
    const list = screen.getByRole('list', { name: 'Controllers' });
    const [first, second] = within(list).getAllByRole('listitem');
    expect(first).toHaveTextContent('kustomize-controller 1 of 2 available');
    expect(second).toHaveTextContent('source-controller healthy');
  });

  it('says so when the cluster serves no Flux kinds and has no controllers', () => {
    renderApp(<FluxDashboardView cluster="c" rows={[]} controllers={[]} />);
    expect(screen.getByText('This cluster serves no Flux kinds.')).toBeInTheDocument();
    expect(screen.getByText(/No Flux controllers/)).toBeInTheDocument();
  });
});

const discovery = {
  serverVersion: 'v1.35.0',
  resources: [
    {
      group: 'kustomize.toolkit.fluxcd.io',
      version: 'v1',
      resource: 'kustomizations',
      kind: 'Kustomization',
      namespaced: true,
      verbs: ['list'],
      preferred: true,
      servedVersions: ['v1'],
    },
    {
      group: 'keda.sh',
      version: 'v1alpha1',
      resource: 'scaledobjects',
      kind: 'ScaledObject',
      namespaced: true,
      verbs: ['list'],
      preferred: true,
      servedVersions: ['v1alpha1'],
    },
  ],
};

const ks = (name: string, ready: string) => ({
  metadata: { uid: name, name, namespace: 'flux-system', creationTimestamp: '2026-11-14T10:00:00Z' },
  spec: { sourceRef: { kind: 'GitRepository', name: 'repo' } },
  status: {
    conditions: [{ type: 'Ready', status: ready, message: ready === 'False' ? 'build failed' : 'ok' }],
  },
});

describe('FluxPage', () => {
  it('counts the objects of the served kinds, shows the controllers and draws the graph', async () => {
    const store = makeLiveStore();
    const client = fakeClient({
      'GET /clusters/c/discovery': () => jsonResponse(discovery),
      'GET /clusters/c/flux/inventory?group=kustomize.toolkit.fluxcd.io&kind=Kustomization&namespace=flux-system&name=apps':
        () =>
          jsonResponse({
            entries: [{ group: 'apps', version: 'v1', kind: 'Deployment', namespace: 'shop', name: 'web' }],
          }),
      'GET /clusters/c/flux/inventory?group=kustomize.toolkit.fluxcd.io&kind=Kustomization&namespace=flux-system&name=broken':
        () => jsonResponse({ entries: [] }),
    });
    renderApp(
      <LiveContext.Provider value={store}>
        <Routes>
          <Route path="/c/:cluster/flux" element={<FluxPage cluster="c" />} />
        </Routes>
      </LiveContext.Provider>,
      { client, route: '/c/c/flux' },
    );
    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
    act(() => FakeWebSocket.last.serverOpen());
    await waitFor(() =>
      expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.open')).toHaveLength(2),
    );
    const opened = FakeWebSocket.last.frames.filter((f) => f.op === 'watch.open') as {
      ch: number;
      d: { resource: string };
    }[];
    const channel = (resource: string) => opened.find((f) => f.d.resource === resource)?.ch ?? -1;
    act(() => {
      FakeWebSocket.last.serverSend({
        ch: channel('kustomizations'),
        t: 'snapshot',
        d: { items: [ks('apps', 'True'), ks('broken', 'False')], more: false },
      });
      FakeWebSocket.last.serverSend({
        ch: channel('deployments'),
        t: 'snapshot',
        d: {
          items: [
            {
              metadata: {
                uid: 'd',
                name: 'kustomize-controller',
                namespace: 'flux-system',
                labels: { 'app.kubernetes.io/part-of': 'flux', 'app.kubernetes.io/version': 'v2.7.0' },
              },
              spec: { replicas: 1 },
              status: { availableReplicas: 1 },
            },
          ],
          more: false,
        },
      });
    });
    const row = await screen.findByRole('row', { name: /Kustomization/ });
    await waitFor(() =>
      expect(
        within(row)
          .getAllByRole('cell')
          .map((c) => c.textContent),
      ).toEqual(['Kustomization', '2', '1', '1', '0', '0']),
    );
    expect(screen.getByText(/Flux v2\.7\.0/)).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'ScaledObject' })).toBeNull();
    expect(await screen.findByRole('button', { name: 'Kustomization broken, failed' })).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: /Deployment web/ })).toBeInTheDocument();
  });
});
