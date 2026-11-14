import { describe, expect, it } from 'vitest';
import { expectToleratesMissingFields, meta, row } from '../testing';
import {
  eventStatus,
  eventsKind,
  leasesKind,
  mutatingWebhooksKind,
  namespacesKind,
  nodeStatus,
  nodesKind,
  validatingWebhooksKind,
} from './cluster';

const ready = (status: string) => [{ type: 'Ready', status }];

describe('Node', () => {
  const node = (extra: Record<string, unknown> = {}) => ({
    metadata: meta('n1', undefined, {
      labels: {
        'node-role.kubernetes.io/worker': '',
        'node-role.kubernetes.io/control-plane': '',
        other: 'x',
      },
    }),
    spec: {},
    status: {
      conditions: [
        ...ready('True'),
        { type: 'MemoryPressure', status: 'True' },
        { type: 'DiskPressure', status: 'False' },
      ],
      nodeInfo: { kubeletVersion: 'v1.35.1' },
      addresses: [
        { type: 'Hostname', address: 'n1' },
        { type: 'InternalIP', address: '10.0.0.5' },
      ],
      allocatable: { cpu: '3920m', memory: '15Gi' },
    },
    ...extra,
  });

  it('shows status, roles, version, IP, allocatable resources and pressure', () => {
    expect(row(nodesKind, node())).toEqual({
      name: 'n1',
      status: 'Ready',
      roles: 'control-plane,worker',
      version: 'v1.35.1',
      internalIP: '10.0.0.5',
      cpu: '3.9',
      memory: '15Gi',
      pressure: 'MemoryPressure',
      age: '1h',
    });
  });

  it('status: NotReady, SchedulingDisabled', () => {
    expect(nodeStatus({ status: { conditions: ready('False') } })).toEqual({
      text: 'NotReady',
      severity: 'error',
    });
    expect(nodeStatus({ status: {} }).text).toBe('NotReady');
    expect(nodeStatus(node({ spec: { unschedulable: true } }))).toEqual({
      text: 'SchedulingDisabled',
      severity: 'warning',
    });
    expect(nodeStatus(node()).severity).toBe('ok');
  });

  it('tolerates missing fields', () => {
    expectToleratesMissingFields(nodesKind);
    expect(row(nodesKind, { metadata: meta('n') })).toMatchObject({
      roles: '',
      internalIP: '',
      cpu: '',
      memory: '',
    });
  });
});

describe('other cluster kinds', () => {
  it('Namespace shows its phase', () => {
    expect(row(namespacesKind, { metadata: meta('a'), status: { phase: 'Active' } }).status).toBe('Active');
    expectToleratesMissingFields(namespacesKind);
  });

  it('Event shows type, reason, object, message, count and the last time seen', () => {
    const ev = {
      metadata: meta('e', 'ns'),
      type: 'Warning',
      reason: 'BackOff',
      regarding: { kind: 'Pod', name: 'web-1' },
      note: 'Back-off restarting',
      series: { count: 7, lastObservedTime: '2026-11-14T11:58:00Z' },
    };
    expect(row(eventsKind, ev)).toMatchObject({
      type: 'Warning',
      reason: 'BackOff',
      object: 'Pod/web-1',
      message: 'Back-off restarting',
      count: 7,
      lastSeen: '2m',
    });
    expect(eventStatus(ev)).toEqual({ text: 'Warning', severity: 'warning' });
    expect(eventStatus({ type: 'Normal' }).severity).toBe('ok');
    const old = {
      metadata: meta('e', 'ns'),
      deprecatedCount: 3,
      deprecatedLastTimestamp: '2026-11-14T11:30:00Z',
    };
    expect(row(eventsKind, old)).toMatchObject({ count: 3, lastSeen: '30m', object: '' });
    expect(row(eventsKind, { metadata: meta('e', 'ns'), eventTime: '2026-11-14T11:45:00Z' }).lastSeen).toBe(
      '15m',
    );
    expect(row(eventsKind, { metadata: meta('e', 'ns') })).toMatchObject({ count: 1, lastSeen: '1h' });
    expectToleratesMissingFields(eventsKind);
  });

  it('Lease shows holder and renew time', () => {
    expect(
      row(leasesKind, {
        metadata: meta('l', 'ns'),
        spec: { holderIdentity: 'node-1', renewTime: '2026-11-14T11:59:30Z' },
      }),
    ).toMatchObject({ holder: 'node-1', renewTime: '30s' });
    expectToleratesMissingFields(leasesKind);
  });

  it('webhook configurations count their webhooks', () => {
    for (const kind of [mutatingWebhooksKind, validatingWebhooksKind]) {
      expect(row(kind, { metadata: meta('w'), webhooks: [{}, {}] }).webhooks).toBe(2);
      expectToleratesMissingFields(kind);
    }
  });
});
