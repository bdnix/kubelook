import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { describe, expect, it } from 'vitest';
import { NodeClaimDetails } from './NodeClaimDetails';
import { NodeKarpenterSection } from './NodeKarpenterSection';
import { KarpenterOverview } from './Overview';
import { ScalingDecisions } from './ScalingDecisions';
import { Topology } from './Topology';

const node = (
  name: string,
  pool: string,
  zone: string,
  type: string,
  ready = 'True',
  over: Record<string, unknown> = {},
) => ({
  metadata: {
    name,
    labels: {
      'karpenter.sh/nodepool': pool,
      'topology.kubernetes.io/zone': zone,
      'node.kubernetes.io/instance-type': type,
      'karpenter.sh/capacity-type': 'spot',
    },
  },
  status: {
    capacity: { cpu: '4', memory: '16Gi', pods: '58' },
    conditions: [{ type: 'Ready', status: ready }],
  },
  ...over,
});
const pool = (name: string, ready = 'True', limits: Record<string, string> = {}) => ({
  metadata: { name },
  spec: { limits, weight: 1, template: { spec: { nodeClassRef: { name: 'ec2' }, expireAfter: '720h' } } },
  status: { nodes: 2, conditions: [{ type: 'Ready', status: ready }] },
});
const claim = (name: string, nodeName?: string) => ({
  metadata: { name, labels: { 'karpenter.sh/nodepool': 'default' } },
  status: { ...(nodeName && { nodeName }), conditions: [] },
});

const wrap = (ui: React.ReactElement) => render(<MemoryRouter>{ui}</MemoryRouter>);

const nodes = [
  node('a', 'default', 'z1', 'm5.large'),
  node('b', 'default', 'z2', 'm5.large'),
  node('c', 'batch', 'z1', 'c5.large', 'False'),
  { metadata: { name: 'plain' }, status: { conditions: [{ type: 'Ready', status: 'True' }] } },
];

describe('KarpenterOverview', () => {
  const view = (extra: Partial<React.ComponentProps<typeof KarpenterOverview>> = {}) =>
    wrap(
      <KarpenterOverview
        cluster="c"
        nodes={nodes}
        pools={[pool('default', 'True', { cpu: '16', memory: '64Gi' }), pool('batch', 'False')]}
        claims={[claim('c1', 'a'), claim('c2')]}
        overlays={[]}
        events={[]}
        now={0}
        {...extra}
      />,
    );

  it('counts the nodes, the pools and the claims that have no node', () => {
    view();
    const totals = screen.getByRole('region', { name: 'Totals' });
    expect(within(totals).getByText('Nodes')).toHaveTextContent('4');
    expect(within(totals).getByText('Managed by Karpenter')).toHaveTextContent('3');
    expect(within(totals).getByText('Other nodes')).toHaveTextContent('1');
    expect(within(totals).getByText('NodePools ready')).toHaveTextContent('1 of 2');
    expect(within(totals).getByText('NodeClaims without a node')).toHaveTextContent('1');
  });

  it('shows a card per pool with its node class, instance types, limits and expiry', () => {
    view();
    const card = screen.getByRole('region', { name: 'NodePool default' });
    expect(card).toHaveTextContent('ec2');
    expect(card).toHaveTextContent('m5.large');
    expect(card).toHaveTextContent('720h');
    expect(within(card).getByRole('progressbar', { name: 'CPU' })).toHaveAttribute('aria-valuenow', '8');
    expect(within(card).getByRole('progressbar', { name: 'CPU' })).toHaveAttribute('aria-valuemax', '16');
    expect(screen.getByRole('region', { name: 'NodePool batch' })).toHaveTextContent('not ready');
  });

  it('lists the overlays that match a pool, and the rejected ones', () => {
    view({
      overlays: [
        {
          metadata: { name: 'cheap' },
          spec: {
            requirements: [{ key: 'node.kubernetes.io/instance-type', operator: 'In', values: ['m5.large'] }],
          },
          status: { conditions: [{ type: 'Ready', status: 'True' }] },
        },
        {
          metadata: { name: 'broken' },
          spec: {
            requirements: [{ key: 'node.kubernetes.io/instance-type', operator: 'In', values: ['m5.large'] }],
          },
          status: { conditions: [{ type: 'Ready', status: 'False', message: 'bad price' }] },
        },
      ],
    });
    const card = screen.getByRole('region', { name: 'NodePool default' });
    expect(within(card).getByText('cheap')).toBeInTheDocument();
    expect(within(card).getByText(/broken/)).toHaveTextContent('rejected');
  });

  it('filters by name, instance type and pool status', async () => {
    view();
    await userEvent.type(screen.getByRole('textbox', { name: 'Filter by name' }), 'bat');
    expect(screen.queryByRole('region', { name: 'NodePool default' })).toBeNull();
    expect(screen.getByRole('region', { name: 'NodePool batch' })).toBeInTheDocument();
    await userEvent.clear(screen.getByRole('textbox', { name: 'Filter by name' }));
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Pool status' }), 'notReady');
    expect(screen.queryByRole('region', { name: 'NodePool default' })).toBeNull();
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Pool status' }), '');
    await userEvent.type(screen.getByRole('textbox', { name: 'Filter by instance type' }), 'c5');
    expect(screen.queryByRole('region', { name: 'NodePool default' })).toBeNull();
    expect(screen.getByRole('region', { name: 'NodePool batch' })).toBeInTheDocument();
  });

  it('says so when there are no pools', () => {
    view({ pools: [] });
    expect(screen.getByText('There are no NodePools.')).toBeInTheDocument();
  });
});

describe('Topology', () => {
  it('draws one rectangle per node, grouped, and regroups when the grouping changes', async () => {
    wrap(<Topology nodes={nodes} />);
    const map = screen.getByRole('img', { name: 'Node topology' });
    expect(map.querySelectorAll('rect[data-node]')).toHaveLength(3);
    expect(screen.getByText('default')).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Group by' }), 'zone');
    expect(screen.getByText('z1')).toBeInTheDocument();
    expect(screen.queryByText('default')).toBeNull();
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Size by' }), 'equal');
    expect(map.querySelectorAll('rect[data-node]')).toHaveLength(3);
  });

  it('says so without Karpenter nodes', () => {
    wrap(<Topology nodes={[]} />);
    expect(screen.getByText('There are no nodes to show.')).toBeInTheDocument();
  });
});

describe('ScalingDecisions', () => {
  const ev = (reason: string, type = 'Normal', msg = 'm') => ({
    type,
    reason,
    message: msg,
    lastTimestamp: '2026-11-20T09:00:00Z',
    involvedObject: { kind: 'NodeClaim', name: 'c1' },
  });
  it('groups the events by what they are about and says the list is only what the cluster holds', async () => {
    wrap(
      <ScalingDecisions
        events={[
          ev('Launched'),
          ev('Consolidating'),
          ev('Drifted'),
          ev('InsufficientCapacity', 'Warning'),
          ev('Whatever'),
        ]}
        now={Date.parse('2026-11-20T10:00:00Z')}
      />,
    );
    expect(screen.getByText(/only the events the cluster still holds/i)).toBeInTheDocument();
    for (const [name, count] of [
      ['Scale up', 1],
      ['Scale down', 1],
      ['Drift', 1],
      ['Errors', 1],
      ['Other', 1],
    ] as const) {
      expect(screen.getByRole('tab', { name: new RegExp(`${name} \\(${count}\\)`) })).toBeInTheDocument();
    }
    await userEvent.click(screen.getByRole('tab', { name: /Errors/ }));
    expect(screen.getByRole('list', { name: 'Errors' })).toHaveTextContent('InsufficientCapacity');
  });
});

describe('NodeClaimDetails', () => {
  it('shows the lifecycle conditions, the facts, the links and why it is not ready', () => {
    wrap(
      <NodeClaimDetails
        cluster="c"
        object={{
          metadata: {
            name: 'c1',
            labels: {
              'karpenter.sh/nodepool': 'default',
              'node.kubernetes.io/instance-type': 'm5.large',
              'karpenter.sh/capacity-type': 'spot',
              'topology.kubernetes.io/zone': 'z1',
            },
          },
          spec: { nodeClassRef: { kind: 'EC2NodeClass', group: 'karpenter.k8s.aws', name: 'ec2' } },
          status: {
            nodeName: 'ip-1',
            providerID: 'aws:///z1/i-1',
            imageID: 'ami-1',
            capacity: { cpu: '2' },
            conditions: [
              { type: 'Launched', status: 'True' },
              { type: 'Registered', status: 'False', message: 'node did not register' },
              { type: 'Initialized', status: 'False', message: 'later' },
              { type: 'Drifted', status: 'True', reason: 'AMIDrift' },
            ],
          },
        }}
      />,
    );
    expect(screen.getByText('Why it is not ready')).toBeInTheDocument();
    expect(screen.getByText('node did not register')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'ip-1' })).toHaveAttribute('href', '/c/c/core/Node/ip-1');
    expect(screen.getByRole('link', { name: 'default' })).toHaveAttribute(
      'href',
      '/c/c/karpenter.sh/NodePool/default',
    );
    expect(screen.getByRole('link', { name: 'ec2' })).toHaveAttribute(
      'href',
      '/c/c/karpenter.k8s.aws/EC2NodeClass/ec2',
    );
    expect(screen.getByText('ami-1')).toBeInTheDocument();
    expect(screen.getByText(/Drifted/)).toBeInTheDocument();
  });
});

describe('NodeKarpenterSection', () => {
  const managed = {
    metadata: {
      name: 'ip-1',
      labels: {
        'karpenter.sh/nodepool': 'default',
        'node.kubernetes.io/instance-type': 'm5.large',
        'karpenter.sh/capacity-type': 'on-demand',
        'topology.kubernetes.io/zone': 'z1',
      },
      annotations: { 'karpenter.sh/do-not-disrupt': 'true' },
    },
    spec: { taints: [{ key: 'karpenter.sh/disrupted', effect: 'NoSchedule' }], providerID: 'aws:///z1/i-1' },
    status: { conditions: [{ type: 'Ready', status: 'True' }] },
  };
  it('shows the pool, the instance and capacity type, the zone, do-not-disrupt and the disruption taint', () => {
    wrap(<NodeKarpenterSection cluster="c" node={managed} />);
    expect(screen.getByRole('link', { name: 'default' })).toHaveAttribute(
      'href',
      '/c/c/karpenter.sh/NodePool/default',
    );
    expect(screen.getByText('m5.large')).toBeInTheDocument();
    expect(screen.getByText('on-demand')).toBeInTheDocument();
    expect(screen.getByText('z1')).toBeInTheDocument();
    expect(screen.getByText(/do-not-disrupt/)).toBeInTheDocument();
    expect(screen.getByText(/being disrupted/i)).toBeInTheDocument();
  });
  it('renders nothing for a node Karpenter does not manage', () => {
    const { container } = wrap(<NodeKarpenterSection cluster="c" node={{ metadata: { name: 'x' } }} />);
    expect(container).toBeEmptyDOMElement();
  });
});
