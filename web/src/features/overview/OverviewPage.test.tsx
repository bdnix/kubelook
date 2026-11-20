import { act, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { LiveContext } from '../../state/useLive';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { makeLiveStore } from '../../test/live';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { OverviewPage } from './OverviewPage';
import { OverviewView } from './OverviewView';

const pod = (name: string, status: Record<string, unknown>) => ({
  metadata: { name, namespace: 'shop', uid: name },
  status,
});
const pods = [
  pod('crash', {
    phase: 'Running',
    containerStatuses: [{ name: 'a', ready: false, state: { waiting: { reason: 'CrashLoopBackOff' } } }],
  }),
  pod('waiting', { phase: 'Pending' }),
  pod('slow', {
    phase: 'Running',
    conditions: [{ type: 'Ready', status: 'False', message: 'not ready yet' }],
    containerStatuses: [{ name: 'app', ready: false }],
  }),
];
const nodes = [
  { metadata: { name: 'n1' }, spec: {}, status: { conditions: [{ type: 'Ready', status: 'True' }] } },
  {
    metadata: { name: 'n2' },
    spec: { unschedulable: true },
    status: {
      conditions: [
        { type: 'Ready', status: 'False' },
        { type: 'MemoryPressure', status: 'True' },
      ],
    },
  },
];
const events = [
  {
    type: 'Warning',
    reason: 'FailedScheduling',
    message: '0/2 nodes are available',
    lastTimestamp: '2026-11-20T09:55:00Z',
    involvedObject: { kind: 'Pod', name: 'waiting', namespace: 'shop' },
  },
  {
    type: 'Warning',
    reason: 'BackOff',
    message: 'back-off',
    lastTimestamp: '2026-11-20T09:58:00Z',
    involvedObject: { kind: 'Pod', name: 'crash', namespace: 'shop' },
  },
];
const now = Date.parse('2026-11-20T10:00:00Z');

describe('OverviewView', () => {
  it('shows the node counts, the failing, pending and not ready pods and the timeline', () => {
    renderApp(<OverviewView cluster="c" now={now} pods={pods} nodes={nodes} events={events} />);
    const nodesSection = screen.getByRole('region', { name: 'Nodes' });
    expect(nodesSection).toHaveTextContent('Ready 1');
    expect(nodesSection).toHaveTextContent('Not ready 1');
    expect(nodesSection).toHaveTextContent('Cordoned 1');
    expect(nodesSection).toHaveTextContent('Under pressure 1');
    expect(within(nodesSection).getByRole('link', { name: '2 nodes' })).toHaveAttribute(
      'href',
      '/c/c/core/Node',
    );
    const failing = screen.getByRole('region', { name: 'Failing pods' });
    expect(failing).toHaveTextContent('crash');
    expect(failing).toHaveTextContent('CrashLoopBackOff');
    expect(within(failing).getByRole('link', { name: 'crash' })).toHaveAttribute(
      'href',
      '/c/c/ns/shop/core/Pod/crash',
    );
    expect(screen.getByRole('region', { name: 'Pending pods' })).toHaveTextContent('0/2 nodes are available');
    const notReady = screen.getByRole('region', { name: 'Pods that are not ready' });
    expect(notReady).toHaveTextContent('not ready yet');
    expect(notReady).toHaveTextContent('app');
    expect(screen.getByRole('list', { name: 'Warnings per five minutes' }).children.length).toBeGreaterThan(
      0,
    );
  });

  it('links each number to the list it counts, filtered where the filter language can say it', () => {
    renderApp(<OverviewView cluster="c" now={now} pods={pods} nodes={nodes} events={events} />);
    expect(
      within(screen.getByRole('region', { name: 'Pending pods' })).getByRole('link', { name: '1 pending' }),
    ).toHaveAttribute('href', `/c/c/core/Pod?filter=${encodeURIComponent('status.phase = Pending')}`);
    expect(
      within(screen.getByRole('region', { name: 'Failing pods' })).getByRole('link', { name: '1 failing' }),
    ).toHaveAttribute('href', '/c/c/core/Pod');
  });

  it('shows the failure counts of the modules that are there and no more', () => {
    renderApp(
      <OverviewView
        cluster="c"
        now={now}
        pods={[]}
        nodes={[]}
        events={[]}
        flux={{ failed: 2 }}
        helm={{ failed: 1 }}
        vaultSecrets={{ failed: 3, total: 5 }}
        karpenter={{ pools: 2, ready: 1, unbound: 4 }}
      />,
    );
    const counts = screen.getByRole('region', { name: 'Failures' });
    expect(within(counts).getByRole('link', { name: '2 failing Flux objects' })).toHaveAttribute(
      'href',
      '/c/c/flux',
    );
    expect(within(counts).getByRole('link', { name: '1 failed Helm release' })).toHaveAttribute(
      'href',
      '/c/c/helm',
    );
    expect(within(counts).getByRole('link', { name: '3 of 5 VaultSecrets failed' })).toHaveAttribute(
      'href',
      '/c/c/ricoberger.de/VaultSecret',
    );
    expect(
      within(counts).getByRole('link', {
        name: 'Karpenter: 1 of 2 NodePools ready, 4 NodeClaims without a node',
      }),
    ).toHaveAttribute('href', '/c/c/modules/karpenter');
  });

  it('hides the sections that were not given', () => {
    renderApp(<OverviewView cluster="c" now={now} />);
    for (const name of ['Nodes', 'Failing pods', 'Pending pods', 'Pods that are not ready', 'Failures'])
      expect(screen.queryByRole('region', { name })).toBeNull();
  });
});

const served = (group: string, resource: string, kind: string, version = 'v1') => ({
  group,
  version,
  resource,
  kind,
  namespaced: true,
  verbs: ['list', 'watch'],
  preferred: true,
  servedVersions: [version],
});
const discovery = {
  serverVersion: 'v1.35.0',
  resources: [
    served('', 'pods', 'Pod'),
    served('', 'nodes', 'Node'),
    served('kustomize.toolkit.fluxcd.io', 'kustomizations', 'Kustomization'),
    served('helm.toolkit.fluxcd.io', 'helmreleases', 'HelmRelease', 'v2'),
    served('ricoberger.de', 'vaultsecrets', 'VaultSecret'),
    served('karpenter.sh', 'nodepools', 'NodePool'),
    served('karpenter.sh', 'nodeclaims', 'NodeClaim'),
  ],
};
const kinds = (g: string, v: string, r: string, k: string) => ({
  group: g,
  version: v,
  resource: r,
  kind: k,
  namespaced: true,
});
const modules = [
  {
    name: 'flux',
    active: true,
    kinds: [
      kinds('kustomize.toolkit.fluxcd.io', 'v1', 'kustomizations', 'Kustomization'),
      kinds('helm.toolkit.fluxcd.io', 'v2', 'helmreleases', 'HelmRelease'),
    ],
  },
  {
    name: 'vaultSecrets',
    active: true,
    kinds: [kinds('ricoberger.de', 'v1', 'vaultsecrets', 'VaultSecret')],
  },
  {
    name: 'karpenter',
    active: true,
    kinds: [
      kinds('karpenter.sh', 'v1', 'nodepools', 'NodePool'),
      kinds('karpenter.sh', 'v1', 'nodeclaims', 'NodeClaim'),
    ],
  },
];

function mount(opts: { allowed?: (resource: string) => boolean; modules?: unknown[] } = {}) {
  const store = makeLiveStore();
  const client = fakeClient({
    'GET /clusters/c/discovery': () => jsonResponse(discovery),
    'GET /clusters/c/modules': () => jsonResponse({ modules: opts.modules ?? modules }),
    'GET /clusters/c/helm/releases': () =>
      jsonResponse({
        releases: [
          { name: 'a', namespace: 'n', status: 'failed' },
          { name: 'b', namespace: 'n', status: 'deployed' },
        ],
      }),
    'POST /clusters/c/access': () =>
      jsonResponse({ results: [{ allowed: true }, { allowed: true }, { allowed: true }] }),
  });
  return {
    ...renderApp(
      <LiveContext.Provider value={store}>
        <OverviewPage cluster="c" />
      </LiveContext.Provider>,
      { client },
    ),
    client,
  };
}

describe('OverviewPage', () => {
  it('opens at most eight watches with every module active, shows the counts, and closes them all on leaving', async () => {
    const { unmount } = mount();
    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
    act(() => FakeWebSocket.last.serverOpen());
    await waitFor(() =>
      expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.open').length).toBe(8),
    );
    const opened = FakeWebSocket.last.frames.filter((f) => f.op === 'watch.open') as {
      ch: number;
      d: { resource: string; namespace: string };
    }[];
    expect(opened.every((f) => f.d.namespace === '')).toBe(true);
    expect(new Set(opened.map((f) => f.d.resource))).toEqual(
      new Set([
        'pods',
        'nodes',
        'events',
        'kustomizations',
        'helmreleases',
        'vaultsecrets',
        'nodepools',
        'nodeclaims',
      ]),
    );
    const send = (resource: string, items: unknown[]) =>
      act(() =>
        FakeWebSocket.last.serverSend({
          ch: opened.find((f) => f.d.resource === resource)?.ch ?? -1,
          t: 'snapshot',
          d: { items, more: false },
        }),
      );
    send('pods', pods);
    send('nodes', nodes);
    send('events', events);
    send('kustomizations', [{ status: { conditions: [{ type: 'Ready', status: 'False' }] } }]);
    send('helmreleases', []);
    send('vaultsecrets', [
      { metadata: { name: 'v' }, status: { conditions: [{ type: 'SecretCreated', status: 'False' }] } },
    ]);
    send('nodepools', [
      { metadata: { name: 'p' }, status: { conditions: [{ type: 'Ready', status: 'True' }] } },
    ]);
    send('nodeclaims', [{ metadata: { name: 'c' }, status: {} }]);
    expect(await screen.findByRole('link', { name: '1 failing' })).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: '1 failing Flux objects' })).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: '1 failed Helm release' })).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: '1 of 1 VaultSecrets failed' })).toBeInTheDocument();
    unmount();
    await waitFor(() =>
      expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.close').length).toBe(8),
    );
  });

  it('opens only the three core watches when no module is active', async () => {
    mount({ modules: [] });
    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
    act(() => FakeWebSocket.last.serverOpen());
    await waitFor(() =>
      expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.open').length).toBe(3),
    );
  });
});
