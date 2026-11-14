import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../../test/render';
import { Conditions } from './Conditions';
import { ContainerList } from './ContainerList';
import { EventsTab } from './EventsTab';
import { OverviewTab } from './OverviewTab';
import { OwnerChain } from './OwnerChain';
import { RelatedTab } from './RelatedTab';

const now = Date.parse('2026-11-14T12:00:00Z');

describe('Conditions', () => {
  it('lists type, status, reason, message and the age of the last transition', () => {
    renderApp(
      <Conditions
        now={now}
        conditions={[
          {
            type: 'Ready',
            status: 'True',
            reason: 'AllUp',
            message: 'fine',
            lastTransitionTime: '2026-11-14T11:00:00Z',
          },
        ]}
      />,
    );
    const row = within(screen.getByRole('table', { name: 'Conditions' })).getAllByRole(
      'row',
    )[1] as HTMLElement;
    expect(
      within(row)
        .getAllByRole('cell')
        .map((c) => c.textContent),
    ).toEqual(['Ready', 'True', 'AllUp', 'fine', '1h']);
  });

  it('renders nothing without conditions', () => {
    const { container } = renderApp(<Conditions conditions={[]} now={now} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe('ContainerList', () => {
  const pod = {
    spec: {
      containers: [
        {
          name: 'app',
          image: 'nginx:1',
          ports: [{ containerPort: 80 }, { containerPort: 53, protocol: 'UDP' }],
        },
        { name: 'side', image: 'busybox' },
      ],
      initContainers: [{ name: 'setup', image: 'init:1' }],
    },
    status: {
      containerStatuses: [
        {
          name: 'app',
          restartCount: 3,
          state: { running: {} },
          lastState: { terminated: { reason: 'OOMKilled' } },
        },
        { name: 'side', state: { waiting: { reason: 'ImagePullBackOff' } } },
      ],
      initContainerStatuses: [{ name: 'setup', state: { terminated: { reason: 'Completed' } } }],
    },
  };

  it('shows image, state, restarts, ports and the last termination reason, init containers apart', () => {
    renderApp(<ContainerList pod={pod} />);
    const containers = screen.getByRole('table', { name: 'Containers' });
    const cells = (row: number) =>
      within(within(containers).getAllByRole('row')[row] as HTMLElement)
        .getAllByRole('cell')
        .map((c) => c.textContent);
    expect(cells(1)).toEqual(['app', 'nginx:1', 'Running', '3', '80/TCP, 53/UDP', 'OOMKilled']);
    expect(cells(2)).toEqual(['side', 'busybox', 'Waiting: ImagePullBackOff', '0', '', '']);
    const init = screen.getByRole('table', { name: 'Init containers' });
    expect(within(init).getByText('Terminated: Completed')).toBeInTheDocument();
  });

  it('copes with containers that have no status yet and with unnamed states', () => {
    renderApp(
      <ContainerList
        pod={{
          spec: { containers: [{ name: 'a' }, { name: 'b' }, { name: 'c' }] },
          status: {
            containerStatuses: [
              { name: 'b', state: { waiting: {} } },
              { name: 'c', state: { terminated: {} } },
            ],
          },
        }}
      />,
    );
    const table = screen.getByRole('table', { name: 'Containers' });
    expect(within(table).getByText('Waiting')).toBeInTheDocument();
    expect(within(table).getByText('Terminated')).toBeInTheDocument();
    expect(screen.queryByRole('table', { name: 'Init containers' })).toBeNull();
  });
});

describe('OwnerChain', () => {
  it('links every owner to its details and flags the one that was not found', () => {
    const ref = (kind: string, name: string, apiVersion: string) => ({
      kind,
      name,
      apiVersion,
      uid: `${kind}/${name}`,
      controller: true,
    });
    renderApp(
      <OwnerChain
        cluster="v:prod"
        namespace="ns"
        chain={[
          { ref: ref('ReplicaSet', 'web-abc', 'apps/v1'), object: {} },
          { ref: ref('Deployment', 'web', 'apps/v1') },
        ]}
      />,
    );
    expect(screen.getByRole('link', { name: 'ReplicaSet/web-abc' })).toHaveAttribute(
      'href',
      '/c/v%3Aprod/ns/ns/apps/ReplicaSet/web-abc',
    );
    expect(screen.getByRole('link', { name: 'Deployment/web' })).toBeInTheDocument();
    expect(screen.getByText('(not found)')).toBeInTheDocument();
  });

  it('renders nothing for an empty chain', () => {
    const { container } = renderApp(<OwnerChain cluster="c" namespace="n" chain={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe('EventsTab', () => {
  it('shows the events as a table, or a note when there are none', () => {
    const { rerender } = renderApp(
      <EventsTab
        now={now}
        events={[
          {
            metadata: { uid: '1' },
            type: 'Warning',
            reason: 'BackOff',
            note: 'Back-off',
            series: { count: 4, lastObservedTime: '2026-11-14T11:58:00Z' },
          },
        ]}
      />,
    );
    const cells = within(
      within(screen.getByRole('table', { name: 'Events' })).getAllByRole('row')[1] as HTMLElement,
    ).getAllByRole('cell');
    expect(cells.map((c) => c.textContent)).toEqual(['Warning', 'BackOff', 'Back-off', '4', '2m']);
    rerender(<EventsTab now={now} events={[]} />);
    expect(screen.getByText('No events.')).toBeInTheDocument();
  });

  it('reads core events too', () => {
    renderApp(
      <EventsTab
        now={now}
        events={[
          { metadata: { name: 'e' }, message: 'old style', count: 2, lastTimestamp: '2026-11-14T11:00:00Z' },
        ]}
      />,
    );
    expect(screen.getByText('old style')).toBeInTheDocument();
    expect(screen.getByText('1h')).toBeInTheDocument();
  });
});

const meta = (name: string, ns = 'ns', extra: Record<string, unknown> = {}) => ({
  name,
  namespace: ns,
  uid: `u-${name}`,
  ...extra,
});

function overview(
  kind: string,
  group: string,
  object: Record<string, unknown>,
  pods: Record<string, unknown>[] = [],
) {
  renderApp(
    <OverviewTab
      cluster="v:prod"
      group={group}
      kind={kind}
      object={object}
      now={now}
      chain={[]}
      pods={pods}
    />,
  );
}

describe('OverviewTab', () => {
  it('lists labels, annotations and conditions', () => {
    overview('Pod', '', {
      metadata: meta('p', 'ns', { labels: { b: '2', a: '1' }, annotations: { note: 'hi' } }),
      status: { conditions: [{ type: 'Ready', status: 'True' }] },
      spec: {},
    });
    const labels = within(screen.getByRole('region', { name: 'Labels' }));
    expect(labels.getAllByRole('listitem').map((l) => l.textContent)).toEqual(['a=1', 'b=2']);
    expect(within(screen.getByRole('region', { name: 'Annotations' })).getByText('hi')).toBeInTheDocument();
    expect(screen.getByRole('table', { name: 'Conditions' })).toBeInTheDocument();
  });

  it('truncates long annotation values at 200 characters until "show more"', async () => {
    const long = 'x'.repeat(250);
    overview('ConfigMap', '', { metadata: meta('c', 'ns', { annotations: { big: long } }) });
    expect(screen.queryByText(long)).toBeNull();
    expect(screen.getByText(`${'x'.repeat(200)}…`)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'show more' }));
    expect(screen.getByText(long)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'show less' }));
    expect(screen.getByText(`${'x'.repeat(200)}…`)).toBeInTheDocument();
  });

  it('turns web URLs in annotations into safe links and leaves other schemes as text', () => {
    overview('ConfigMap', '', {
      metadata: meta('c', 'ns', {
        annotations: { docs: 'https://example.com/a', evil: 'javascript:alert(1)' },
      }),
    });
    expect(screen.getByRole('link', { name: 'https://example.com/a' })).toHaveAttribute(
      'rel',
      'noopener noreferrer',
    );
    expect(screen.queryByRole('link', { name: /javascript/ })).toBeNull();
    expect(screen.getByText('javascript:alert(1)')).toBeInTheDocument();
  });

  it('pods show their containers', () => {
    overview('Pod', '', { metadata: meta('p'), spec: { containers: [{ name: 'app', image: 'i' }] } });
    expect(screen.getByRole('table', { name: 'Containers' })).toBeInTheDocument();
  });

  it('workloads show strategy, replicas and selector', () => {
    overview('Deployment', 'apps', {
      apiVersion: 'apps/v1',
      metadata: meta('web'),
      spec: { replicas: 3, strategy: { type: 'RollingUpdate' }, selector: { matchLabels: { app: 'web' } } },
      status: { readyReplicas: 2 },
    });
    const w = within(screen.getByRole('region', { name: 'Workload' }));
    expect(w.getByText('RollingUpdate')).toBeInTheDocument();
    expect(w.getByText('2 ready of 3')).toBeInTheDocument();
    expect(w.getByText('app=web')).toBeInTheDocument();
  });

  it('a DaemonSet shows its update strategy and no replica count', () => {
    overview('DaemonSet', 'apps', {
      apiVersion: 'apps/v1',
      metadata: meta('d'),
      spec: { updateStrategy: { type: 'OnDelete' } },
    });
    const w = within(screen.getByRole('region', { name: 'Workload' }));
    expect(w.getByText('OnDelete')).toBeInTheDocument();
    expect(w.queryByText('Replicas')).toBeNull();
  });

  it('nodes show capacity, allocatable, taints and pressure', () => {
    overview('Node', '', {
      metadata: meta('n1', ''),
      spec: {
        taints: [
          { key: 'dedicated', value: 'gpu', effect: 'NoSchedule' },
          { key: 'x', effect: 'NoExecute' },
        ],
      },
      status: {
        capacity: { cpu: '4', memory: '16Gi' },
        allocatable: { cpu: '3920m', memory: '15Gi' },
        conditions: [
          { type: 'MemoryPressure', status: 'True' },
          { type: 'Ready', status: 'True' },
        ],
      },
    });
    const n = within(screen.getByRole('region', { name: 'Node' }));
    expect(n.getByText('cpu: 4, memory: 16Gi')).toBeInTheDocument();
    expect(n.getByText('cpu: 3920m, memory: 15Gi')).toBeInTheDocument();
    expect(n.getByText('dedicated=gpu:NoSchedule, x:NoExecute')).toBeInTheDocument();
    expect(n.getByText('MemoryPressure')).toBeInTheDocument();
  });

  it('a node without taints or pressure says none', () => {
    overview('Node', '', { metadata: meta('n1', ''), status: {} });
    expect(screen.getAllByText('none')).toHaveLength(2);
  });

  it('services show ports and the pods their selector matches', () => {
    overview(
      'Service',
      '',
      {
        metadata: meta('web'),
        spec: {
          selector: { app: 'web' },
          ports: [
            { name: 'http', port: 80, targetPort: 8080 },
            { port: 53, protocol: 'UDP' },
          ],
        },
      },
      [
        { metadata: meta('web-1', 'ns', { labels: { app: 'web' } }) },
        { metadata: meta('db-1', 'ns', { labels: { app: 'db' } }) },
      ],
    );
    expect(screen.getByText('http: 80→8080/TCP')).toBeInTheDocument();
    expect(screen.getByText('port: 53→/UDP')).toBeInTheDocument();
    const matched = within(screen.getByRole('region', { name: 'Matched pods' }));
    expect(matched.getByRole('link', { name: 'web-1' })).toHaveAttribute(
      'href',
      '/c/v%3Aprod/ns/ns/core/Pod/web-1',
    );
    expect(matched.queryByRole('link', { name: 'db-1' })).toBeNull();
  });

  it('a service without matching pods says so', () => {
    overview('Service', '', { metadata: meta('web'), spec: { selector: { app: 'x' } } });
    expect(
      within(screen.getByRole('region', { name: 'Matched pods' })).getByText('None'),
    ).toBeInTheDocument();
  });

  it('ingresses link their rules to Services', () => {
    overview('Ingress', 'networking.k8s.io', {
      metadata: meta('i'),
      spec: {
        rules: [
          {
            host: 'a.example',
            http: { paths: [{ path: '/api', backend: { service: { name: 'api' } } }, { backend: {} }] },
          },
          { http: { paths: [{ backend: { service: { name: 'web' } } }] } },
        ],
      },
    });
    const rules = within(screen.getByRole('region', { name: 'Rules' }));
    expect(rules.getByRole('link', { name: 'api' })).toHaveAttribute(
      'href',
      '/c/v%3Aprod/ns/ns/core/Service/api',
    );
    expect(rules.getByText(/a.example\/api/)).toBeInTheDocument();
    expect(rules.getByText(/no service/)).toBeInTheDocument();
    expect(rules.getByText(/\*\//)).toBeInTheDocument();
  });

  it('an ingress without backends says so', () => {
    overview('Ingress', 'networking.k8s.io', { metadata: meta('i'), spec: {} });
    expect(screen.getByText('No backends')).toBeInTheDocument();
  });

  it('claims show the bound volume and the pods that mount them', () => {
    overview('PersistentVolumeClaim', '', { metadata: meta('data'), spec: { volumeName: 'pv-1' } }, [
      { metadata: meta('p1'), spec: { volumes: [{ persistentVolumeClaim: { claimName: 'data' } }] } },
      { metadata: meta('p2'), spec: {} },
    ]);
    expect(
      within(screen.getByRole('region', { name: 'Volume' })).getByRole('link', { name: 'pv-1' }),
    ).toHaveAttribute('href', '/c/v%3Aprod/core/PersistentVolume/pv-1');
    const mounted = within(screen.getByRole('region', { name: 'Mounted by' }));
    expect(mounted.getByRole('link', { name: 'p1' })).toBeInTheDocument();
    expect(mounted.queryByRole('link', { name: 'p2' })).toBeNull();
  });

  it('an unbound claim says so', () => {
    overview('PersistentVolumeClaim', '', { metadata: meta('data'), spec: {} });
    expect(screen.getByText('Not bound')).toBeInTheDocument();
  });
});

describe('RelatedTab', () => {
  const ref = { kind: 'ReplicaSet', name: 'web-abc', apiVersion: 'apps/v1', uid: 'rs1', controller: true };

  it('lists owners, children, matched pods and ingress backends as links', () => {
    renderApp(
      <RelatedTab
        cluster="v:prod"
        kind="Service"
        object={{ metadata: meta('web'), spec: { selector: { app: 'web' } } }}
        chain={[{ ref }]}
        children={[{ kind: 'Pod', apiVersion: 'v1', metadata: meta('child-1') }]}
        pods={[{ metadata: meta('web-1', 'ns', { labels: { app: 'web' } }) }]}
      />,
    );
    expect(
      within(screen.getByRole('region', { name: 'Owners' })).getByRole('link', {
        name: 'ReplicaSet/web-abc',
      }),
    ).toHaveAttribute('href', '/c/v%3Aprod/ns/ns/apps/ReplicaSet/web-abc');
    expect(
      within(screen.getByRole('region', { name: 'Children' })).getByRole('link', { name: 'Pod/child-1' }),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole('region', { name: 'Pods' })).getByRole('link', { name: 'Pod/web-1' }),
    ).toBeInTheDocument();
  });

  it('lists the Services of an Ingress', () => {
    renderApp(
      <RelatedTab
        cluster="v:prod"
        kind="Ingress"
        object={{
          metadata: meta('i'),
          spec: { rules: [{ http: { paths: [{ backend: { service: { name: 'api' } } }] } }] },
        }}
        chain={[]}
        children={[]}
        pods={[]}
      />,
    );
    expect(
      within(screen.getByRole('region', { name: 'Services' })).getByRole('link', { name: 'Service/api' }),
    ).toBeInTheDocument();
  });

  it('says so when nothing is related', () => {
    renderApp(
      <RelatedTab
        cluster="c"
        kind="ConfigMap"
        object={{ metadata: meta('c') }}
        chain={[]}
        children={[]}
        pods={[]}
      />,
    );
    expect(screen.getByText('Nothing related.')).toBeInTheDocument();
  });
});
