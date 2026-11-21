import { describe, expect, it } from 'vitest';
import {
  failedFlux,
  failedHelm,
  failingPods,
  nodeHealth,
  notReadyPods,
  pendingPods,
  warningTimeline,
} from './triage';

const pod = (name: string, status: Record<string, unknown>, ns = 'shop') => ({
  metadata: { name, namespace: ns },
  status,
});
const waiting = (reason: string) => ({
  containerStatuses: [{ name: 'app', ready: false, state: { waiting: { reason } } }],
});

describe('failingPods', () => {
  it('has failed pods and pods whose containers wait with a failing reason', () => {
    const got = failingPods([
      pod('failed', { phase: 'Failed', reason: 'Evicted' }),
      pod('crash', { phase: 'Running', ...waiting('CrashLoopBackOff') }),
      pod('pull', { phase: 'Pending', ...waiting('ImagePullBackOff') }),
      pod('errpull', { phase: 'Pending', ...waiting('ErrImagePull') }),
      pod('config', { phase: 'Pending', ...waiting('CreateContainerConfigError') }),
      pod('fine', {
        phase: 'Running',
        containerStatuses: [{ name: 'a', ready: true, state: { running: {} } }],
      }),
      pod('init', { phase: 'Pending', ...waiting('ContainerCreating') }),
    ]);
    expect(got.map((g) => [g.name, g.reason])).toEqual([
      ['failed', 'Evicted'],
      ['crash', 'CrashLoopBackOff'],
      ['pull', 'ImagePullBackOff'],
      ['errpull', 'ErrImagePull'],
      ['config', 'CreateContainerConfigError'],
    ]);
    expect(got[0]).toMatchObject({ namespace: 'shop' });
  });
  it('looks at init containers too', () => {
    const p = pod('i', {
      phase: 'Pending',
      initContainerStatuses: [{ name: 'i', state: { waiting: { reason: 'CrashLoopBackOff' } } }],
    });
    expect(failingPods([p])).toHaveLength(1);
  });
});

describe('pendingPods', () => {
  const ev = (podName: string, message: string, at: string, reason = 'FailedScheduling') => ({
    reason,
    message,
    lastTimestamp: at,
    involvedObject: { kind: 'Pod', name: podName, namespace: 'shop' },
  });
  it('joins each pending pod with the latest FailedScheduling event', () => {
    const got = pendingPods(
      [pod('a', { phase: 'Pending' }), pod('b', { phase: 'Pending' }), pod('c', { phase: 'Running' })],
      [
        ev('a', 'old', '2026-11-20T09:00:00Z'),
        ev('a', '0/3 nodes are available', '2026-11-20T09:30:00Z'),
        ev('a', 'ignored', '2026-11-20T09:59:00Z', 'Scheduled'),
      ],
    );
    expect(got).toEqual([
      { name: 'a', namespace: 'shop', message: '0/3 nodes are available' },
      { name: 'b', namespace: 'shop', message: '' },
    ]);
  });
  it('reads events of the events.k8s.io shape', () => {
    const got = pendingPods(
      [pod('a', { phase: 'Pending' })],
      [
        {
          reason: 'FailedScheduling',
          note: 'no nodes',
          eventTime: '2026-11-20T09:00:00Z',
          regarding: { kind: 'Pod', name: 'a', namespace: 'shop' },
        },
      ],
    );
    expect(got[0]?.message).toBe('no nodes');
  });
});

describe('notReadyPods', () => {
  it('lists running pods that are not ready with the failing condition and the containers that are not ready', () => {
    const got = notReadyPods([
      pod('slow', {
        phase: 'Running',
        conditions: [
          { type: 'Ready', status: 'False', message: 'containers with unready status: [app]' },
          { type: 'ContainersReady', status: 'False' },
        ],
        containerStatuses: [
          { name: 'app', ready: false },
          { name: 'side', ready: true },
        ],
      }),
      pod('fine', {
        phase: 'Running',
        conditions: [{ type: 'Ready', status: 'True' }],
        containerStatuses: [{ name: 'a', ready: true }],
      }),
      pod('pending', { phase: 'Pending', conditions: [{ type: 'Ready', status: 'False' }] }),
    ]);
    expect(got).toEqual([
      {
        name: 'slow',
        namespace: 'shop',
        message: 'containers with unready status: [app]',
        containers: ['app'],
      },
    ]);
  });
  it('falls back to the ContainersReady condition', () => {
    const got = notReadyPods([
      pod('p', {
        phase: 'Running',
        conditions: [{ type: 'ContainersReady', status: 'False', message: 'nope' }],
        containerStatuses: [],
      }),
    ]);
    expect(got[0]?.message).toBe('nope');
  });
});

describe('nodeHealth', () => {
  const node = (ready: string, extra: Record<string, unknown> = {}, pressure: string[] = []) => ({
    spec: extra,
    status: {
      conditions: [{ type: 'Ready', status: ready }, ...pressure.map((t) => ({ type: t, status: 'True' }))],
    },
  });
  it('counts ready, not ready, cordoned and under pressure', () => {
    expect(
      nodeHealth([
        node('True'),
        node('False'),
        node('True', { unschedulable: true }),
        node('True', {}, ['MemoryPressure']),
        node('True', {}, ['DiskPressure', 'PIDPressure']),
      ]),
    ).toEqual({ total: 5, ready: 4, notReady: 1, cordoned: 1, underPressure: 2 });
  });
  it('is zero without nodes', () => {
    expect(nodeHealth([])).toEqual({ total: 0, ready: 0, notReady: 0, cordoned: 0, underPressure: 0 });
  });
});

describe('warningTimeline', () => {
  const warn = (at: string, type = 'Warning') => ({ type, lastTimestamp: at, reason: 'x' });
  it('buckets the warnings per five minutes, up to the injected now, with empty buckets in between', () => {
    const now = Date.parse('2026-11-20T10:07:00Z');
    const got = warningTimeline(
      [
        warn('2026-11-20T09:51:00Z'),
        warn('2026-11-20T09:53:00Z'),
        warn('2026-11-20T10:06:00Z'),
        warn('2026-11-20T10:00:00Z', 'Normal'),
      ],
      now,
    );
    expect(got.map((b) => [new Date(b.start).toISOString(), b.count])).toEqual([
      ['2026-11-20T09:50:00.000Z', 2],
      ['2026-11-20T09:55:00.000Z', 0],
      ['2026-11-20T10:00:00.000Z', 0],
      ['2026-11-20T10:05:00.000Z', 1],
    ]);
  });
  it('is empty without warnings', () => {
    expect(
      warningTimeline(
        [{ type: 'Normal', lastTimestamp: '2026-11-20T10:00:00Z' }],
        Date.parse('2026-11-20T10:07:00Z'),
      ),
    ).toEqual([]);
  });
  it('reads eventTime and creationTimestamp when there is no lastTimestamp', () => {
    const now = Date.parse('2026-11-20T10:07:00Z');
    expect(
      warningTimeline(
        [
          { type: 'Warning', eventTime: '2026-11-20T10:06:00Z' },
          { type: 'Warning', metadata: { creationTimestamp: '2026-11-20T10:01:00Z' } },
        ],
        now,
      ).map((b) => b.count),
    ).toEqual([1, 1]);
  });
});

describe('failure counts', () => {
  it('counts Flux objects whose Ready condition is False', () => {
    expect(
      failedFlux([
        { status: { conditions: [{ type: 'Ready', status: 'False' }] } },
        { status: { conditions: [{ type: 'Ready', status: 'True' }] } },
        {
          spec: { suspend: true },
          status: { conditions: [{ type: 'Ready', status: 'False' }] },
          kind: 'Kustomization',
        },
      ]),
    ).toBe(2);
  });
  it('counts Helm releases that failed or are pending', () => {
    expect(
      failedHelm([
        { status: 'deployed' },
        { status: 'failed' },
        { status: 'pending-upgrade' },
        { status: 'pending-install' },
        { status: 'superseded' },
      ]),
    ).toBe(3);
  });
});
