import { describe, expect, it } from 'vitest';
import { expectToleratesMissingFields, meta, row } from '../testing';
import {
  cronJobsKind,
  daemonSetsKind,
  deploymentStatus,
  deploymentsKind,
  jobStatus,
  jobsKind,
  replicaSetsKind,
  statefulSetStatus,
  statefulSetsKind,
} from './workloads';

const cond = (type: string, status: string) => ({ type, status });

describe('Deployment', () => {
  const d = (spec: object, status: object) => ({ metadata: meta('web', 'ns'), spec, status });

  it('shows ready, up-to-date, available and status columns', () => {
    const obj = d(
      { replicas: 3 },
      {
        readyReplicas: 2,
        updatedReplicas: 3,
        availableReplicas: 2,
        replicas: 3,
        conditions: [cond('Available', 'True')],
      },
    );
    expect(row(deploymentsKind, obj)).toEqual({
      name: 'web',
      namespace: 'ns',
      ready: '2/3',
      upToDate: 3,
      available: 2,
      status: 'Progressing',
      age: '1h',
    });
  });

  it.each([
    [
      'paused wins',
      d({ paused: true, replicas: 1 }, { conditions: [cond('Available', 'False')] }),
      'Paused',
      'warning',
    ],
    ['degraded', d({ replicas: 1 }, { conditions: [cond('Available', 'False')] }), 'Degraded', 'error'],
    [
      'available',
      d(
        { replicas: 2 },
        { replicas: 2, updatedReplicas: 2, availableReplicas: 2, conditions: [cond('Available', 'True')] },
      ),
      'Available',
      'ok',
    ],
    [
      'rolling surge',
      d(
        { replicas: 2 },
        { replicas: 3, updatedReplicas: 2, availableReplicas: 2, conditions: [cond('Available', 'True')] },
      ),
      'Progressing',
      'warning',
    ],
    [
      'no conditions yet',
      d({ replicas: 1 }, { replicas: 1, updatedReplicas: 1, availableReplicas: 1 }),
      'Progressing',
      'warning',
    ],
  ])('status: %s', (_name, obj, text, severity) => {
    expect(deploymentStatus(obj)).toEqual({ text, severity });
  });

  it('defaults to one desired replica', () => {
    expect(row(deploymentsKind, d({}, {})).ready).toBe('0/1');
  });

  it('tolerates missing fields', () => expectToleratesMissingFields(deploymentsKind));
});

describe('StatefulSet', () => {
  it('shows ready and status', () => {
    const ss = (replicas: number, ready: number) => ({
      metadata: meta('db', 'ns'),
      spec: { replicas },
      status: { readyReplicas: ready },
    });
    expect(row(statefulSetsKind, ss(3, 3))).toMatchObject({ ready: '3/3', status: 'Ready' });
    expect(row(statefulSetsKind, ss(3, 1))).toMatchObject({ ready: '1/3', status: 'Progressing' });
    expect(statefulSetStatus(ss(3, 1)).severity).toBe('warning');
    expectToleratesMissingFields(statefulSetsKind);
  });
});

describe('DaemonSet', () => {
  it('shows the scheduling counters and node selector', () => {
    const ds = {
      metadata: meta('agent', 'ns'),
      spec: { template: { spec: { nodeSelector: { disk: 'ssd', arch: 'arm64' } } } },
      status: {
        desiredNumberScheduled: 4,
        currentNumberScheduled: 4,
        numberReady: 3,
        updatedNumberScheduled: 4,
        numberAvailable: 3,
      },
    };
    expect(row(daemonSetsKind, ds)).toMatchObject({
      desired: 4,
      current: 4,
      ready: 3,
      upToDate: 4,
      available: 3,
      nodeSelector: 'arch=arm64,disk=ssd',
    });
    expectToleratesMissingFields(daemonSetsKind);
  });
});

describe('ReplicaSet', () => {
  it('shows counters and the owner', () => {
    const rs = {
      metadata: meta('web-abc', 'ns', { ownerReferences: [{ kind: 'Deployment', name: 'web' }] }),
      spec: { replicas: 2 },
      status: { replicas: 2, readyReplicas: 1 },
    };
    expect(row(replicaSetsKind, rs)).toMatchObject({
      desired: 2,
      current: 2,
      ready: 1,
      owner: 'Deployment/web',
    });
    expect(row(replicaSetsKind, { metadata: meta('x', 'ns') }).owner).toBe('');
    expectToleratesMissingFields(replicaSetsKind);
  });
});

describe('Job', () => {
  const job = (spec: object, status: object) => ({ metadata: meta('j', 'ns'), spec, status });

  it.each([
    ['complete', job({}, { conditions: [cond('Complete', 'True')] }), 'Complete', 'ok'],
    ['failed', job({}, { conditions: [cond('Failed', 'True')] }), 'Failed', 'error'],
    ['suspended', job({ suspend: true }, {}), 'Suspended', 'warning'],
    ['running', job({}, {}), 'Running', 'ok'],
  ])('status: %s', (_n, obj, text, severity) => expect(jobStatus(obj)).toEqual({ text, severity }));

  it('shows completions and a duration that runs until completion, or until now', () => {
    const running = job({ completions: 3 }, { succeeded: 1, startTime: '2026-11-14T11:30:00Z' });
    expect(row(jobsKind, running)).toMatchObject({ completions: '1/3', duration: '30m', status: 'Running' });
    const done = job(
      {},
      { succeeded: 1, startTime: '2026-11-14T11:30:00Z', completionTime: '2026-11-14T11:40:00Z' },
    );
    expect(row(jobsKind, done)).toMatchObject({ completions: '1/1', duration: '10m' });
    expect(row(jobsKind, job({}, {})).duration).toBe('');
    expectToleratesMissingFields(jobsKind);
  });
});

describe('CronJob', () => {
  it('shows schedule, suspend, active jobs and the last schedule', () => {
    const cj = {
      metadata: meta('nightly', 'ns'),
      spec: { schedule: '0 2 * * *', suspend: true },
      status: { active: [{}, {}], lastScheduleTime: '2026-11-14T11:55:00Z' },
    };
    expect(row(cronJobsKind, cj)).toMatchObject({
      schedule: '0 2 * * *',
      suspend: 'true',
      active: 2,
      lastSchedule: '5m',
    });
    expect(row(cronJobsKind, { metadata: meta('n', 'ns'), spec: {} }).suspend).toBe('false');
    expectToleratesMissingFields(cronJobsKind);
  });
});
