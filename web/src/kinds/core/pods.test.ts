import { describe, expect, it } from 'vitest';
import { podReady, podRestarts, podSeverity, podStatus, podsKind } from './pods';

const pod = (status: Record<string, unknown>, extra: Record<string, unknown> = {}) => ({
  metadata: { name: 'web-1', namespace: 'ns', creationTimestamp: '2026-11-14T09:00:00Z' },
  spec: { nodeName: 'node-a', containers: [{ name: 'a' }, { name: 'b' }, { name: 'c' }] },
  status,
  ...extra,
});

const running = { ready: true, restartCount: 0, state: { running: {} } };

describe('podStatus', () => {
  it.each([
    ['Pending', pod({ phase: 'Pending' }), 'Pending'],
    ['Running', pod({ phase: 'Running', containerStatuses: [running] }), 'Running'],
    ['Succeeded', pod({ phase: 'Succeeded' }), 'Succeeded'],
    ['Failed', pod({ phase: 'Failed' }), 'Failed'],
    ['no phase', pod({}), 'Unknown'],
    ['status reason', pod({ phase: 'Failed', reason: 'Evicted' }), 'Evicted'],
    [
      'CrashLoopBackOff',
      pod({
        phase: 'Running',
        containerStatuses: [{ ...running }, { state: { waiting: { reason: 'CrashLoopBackOff' } } }],
      }),
      'CrashLoopBackOff',
    ],
    [
      'ImagePullBackOff',
      pod({ phase: 'Pending', containerStatuses: [{ state: { waiting: { reason: 'ImagePullBackOff' } } }] }),
      'ImagePullBackOff',
    ],
    [
      'terminated with reason',
      pod({
        phase: 'Running',
        containerStatuses: [{ state: { terminated: { reason: 'OOMKilled', exitCode: 137 } } }],
      }),
      'OOMKilled',
    ],
    [
      'terminated by signal',
      pod({ phase: 'Running', containerStatuses: [{ state: { terminated: { signal: 9, exitCode: 137 } } }] }),
      'Signal:9',
    ],
    [
      'terminated with exit code',
      pod({ phase: 'Running', containerStatuses: [{ state: { terminated: { exitCode: 2 } } }] }),
      'ExitCode:2',
    ],
    [
      'Completed but another container runs',
      pod({
        phase: 'Running',
        containerStatuses: [{ state: { terminated: { reason: 'Completed' } } }, running],
      }),
      'Running',
    ],
    [
      'Init 1/2',
      pod(
        {
          phase: 'Pending',
          initContainerStatuses: [
            { state: { terminated: { exitCode: 0 } } },
            { state: { waiting: { reason: 'PodInitializing' } } },
          ],
        },
        { spec: { containers: [{ name: 'a' }], initContainers: [{ name: 'i1' }, { name: 'i2' }] } },
      ),
      'Init:1/2',
    ],
    [
      'Init error',
      pod({
        phase: 'Pending',
        initContainerStatuses: [{ state: { terminated: { reason: 'Error', exitCode: 1 } } }],
      }),
      'Init:Error',
    ],
    [
      'Init exit code',
      pod({ phase: 'Pending', initContainerStatuses: [{ state: { terminated: { exitCode: 3 } } }] }),
      'Init:ExitCode:3',
    ],
    [
      'Init signal',
      pod({
        phase: 'Pending',
        initContainerStatuses: [{ state: { terminated: { signal: 15, exitCode: 143 } } }],
      }),
      'Init:Signal:15',
    ],
    [
      'Init waiting reason',
      pod({
        phase: 'Pending',
        initContainerStatuses: [{ state: { waiting: { reason: 'ImagePullBackOff' } } }],
      }),
      'Init:ImagePullBackOff',
    ],
  ])('%s', (_name, p, want) => {
    expect(podStatus(p)).toBe(want);
  });

  it('is Terminating when a deletion timestamp is set, Unknown when the node was lost', () => {
    const base = { phase: 'Running', containerStatuses: [running] };
    const meta = { name: 'p', deletionTimestamp: '2026-11-14T10:00:00Z' };
    expect(podStatus({ metadata: meta, status: base })).toBe('Terminating');
    expect(podStatus({ metadata: meta, status: { ...base, reason: 'NodeLost' } })).toBe('Unknown');
  });
});

describe('pod columns', () => {
  const p = pod({
    phase: 'Running',
    containerStatuses: [
      running,
      { ...running, ready: false, restartCount: 2 },
      { ...running, restartCount: 5 },
    ],
  });
  const now = Date.parse('2026-11-14T12:00:00Z');

  it('derives Ready, Restarts, Node and Age', () => {
    expect(podReady(p)).toBe('2/3');
    expect(podRestarts(p)).toBe(7);
    const byId = Object.fromEntries(podsKind.columns.map((c) => [c.id, c.value(p, now)]));
    expect(byId).toEqual({
      name: 'web-1',
      namespace: 'ns',
      ready: '2/3',
      status: 'Running',
      restarts: 7,
      node: 'node-a',
      age: '3h',
    });
  });

  it('counts ready containers against the status list when the spec is missing', () => {
    expect(podReady({ status: { containerStatuses: [running] } })).toBe('1/1');
    expect(podReady({})).toBe('0/0');
    expect(podRestarts({})).toBe(0);
  });

  it('describes the kind', () => {
    expect(podsKind).toMatchObject({ group: '', kind: 'Pod', resource: 'pods', namespaced: true });
    expect(podsKind.status?.(p)).toEqual({ text: 'Running', severity: 'ok' });
  });

  it('classifies the severity of a status text', () => {
    expect(podSeverity('Running')).toBe('ok');
    expect(podSeverity('Completed')).toBe('ok');
    expect(podSeverity('Pending')).toBe('warning');
    expect(podSeverity('Terminating')).toBe('warning');
    expect(podSeverity('Init:1/2')).toBe('warning');
    expect(podSeverity('Init:Error')).toBe('error');
    expect(podSeverity('CrashLoopBackOff')).toBe('error');
    expect(podSeverity('ImagePullBackOff')).toBe('error');
    expect(podSeverity('OOMKilled')).toBe('error');
    expect(podSeverity('ExitCode:2')).toBe('error');
    expect(podSeverity('SomethingElse')).toBe('unknown');
  });
});
