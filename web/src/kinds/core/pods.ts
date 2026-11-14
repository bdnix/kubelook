import { formatAge } from '../../lib/age';
import { get, getArray, getNumber, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import type { KindDescriptor, Severity, Status } from '../types';

type Container = Record<string, unknown>;

function containers(pod: K8sObject, path: string): Container[] {
  return getArray(pod, path).filter((c): c is Container => typeof c === 'object' && c !== null);
}

/** `ready/total` of the pod's containers, such as `2/3`. */
export function podReady(pod: K8sObject): string {
  const statuses = containers(pod, 'status.containerStatuses');
  const total = getArray(pod, 'spec.containers').length || statuses.length;
  return `${statuses.filter((c) => c.ready === true).length}/${total}`;
}

export function podRestarts(pod: K8sObject): number {
  return containers(pod, 'status.containerStatuses').reduce(
    (sum, c) => sum + getNumber(c, 'restartCount'),
    0,
  );
}

function initStatus(pod: K8sObject): string | undefined {
  const inits = containers(pod, 'status.initContainerStatuses');
  for (const [i, c] of inits.entries()) {
    const terminated = get(c, 'state.terminated') as Container | undefined;
    if (terminated && getNumber(terminated, 'exitCode') === 0) continue;
    if (terminated) {
      const reason = getString(terminated, 'reason');
      if (reason) return `Init:${reason}`;
      const signal = getNumber(terminated, 'signal');
      return signal ? `Init:Signal:${signal}` : `Init:ExitCode:${getNumber(terminated, 'exitCode')}`;
    }
    const waiting = getString(c, 'state.waiting.reason');
    if (waiting && waiting !== 'PodInitializing') return `Init:${waiting}`;
    return `Init:${i}/${getArray(pod, 'spec.initContainers').length || inits.length}`;
  }
  return undefined;
}

/** The status column of `kubectl get pods`. */
export function podStatus(pod: K8sObject): string {
  let reason = getString(pod, 'status.phase') || 'Unknown';
  const statusReason = getString(pod, 'status.reason');
  if (statusReason) reason = statusReason;

  const init = initStatus(pod);
  if (init) {
    reason = init;
  } else {
    let hasRunning = false;
    const statuses = containers(pod, 'status.containerStatuses');
    for (const c of [...statuses].reverse()) {
      const waiting = getString(c, 'state.waiting.reason');
      const terminated = get(c, 'state.terminated') as Container | undefined;
      if (waiting) {
        reason = waiting;
      } else if (terminated) {
        const t = getString(terminated, 'reason');
        if (t) reason = t;
        else if (getNumber(terminated, 'signal')) reason = `Signal:${getNumber(terminated, 'signal')}`;
        else reason = `ExitCode:${getNumber(terminated, 'exitCode')}`;
      } else if (get(c, 'state.running') !== undefined && c.ready === true) {
        hasRunning = true;
      }
    }
    if (reason === 'Completed' && hasRunning) reason = 'Running';
  }
  if (get(pod, 'metadata.deletionTimestamp') !== undefined) {
    return statusReason === 'NodeLost' ? 'Unknown' : 'Terminating';
  }
  return reason;
}

const ERRORS = /BackOff|Err|Error|Failed|OOMKilled|Evicted|Unknown|Signal:|ExitCode:|Invalid/;

export function podSeverity(text: string): Severity {
  if (text === 'Running' || text === 'Succeeded' || text === 'Completed') return 'ok';
  if (
    text === 'Pending' ||
    text === 'Terminating' ||
    text === 'ContainerCreating' ||
    text.startsWith('Init:')
  ) {
    return /Init:(\d+\/\d+|PodInitializing)/.test(text) || !ERRORS.test(text) ? 'warning' : 'error';
  }
  return ERRORS.test(text) ? 'error' : 'unknown';
}

function status(pod: K8sObject): Status {
  const text = podStatus(pod);
  return { text, severity: podSeverity(text) };
}

export const podsKind: KindDescriptor = {
  group: '',
  kind: 'Pod',
  resource: 'pods',
  namespaced: true,
  title: 'Pods',
  section: 'Workloads',
  status,
  columns: [
    { id: 'name', title: 'Name', value: (o) => getString(o, 'metadata.name') },
    { id: 'namespace', title: 'Namespace', value: (o) => getString(o, 'metadata.namespace') },
    { id: 'ready', title: 'Ready', value: podReady },
    { id: 'status', title: 'Status', value: (o) => podStatus(o) },
    { id: 'restarts', title: 'Restarts', value: podRestarts, sort: 'number' },
    { id: 'node', title: 'Node', value: (o) => getString(o, 'spec.nodeName') },
    {
      id: 'age',
      title: 'Age',
      value: (o, now) => formatAge(getString(o, 'metadata.creationTimestamp'), now),
      sort: 'age',
    },
  ],
};
