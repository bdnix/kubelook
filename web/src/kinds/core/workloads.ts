import { formatAge } from '../../lib/age';
import { getArray, getNumber, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { conditionStatus, defineKind, labelString, numberColumn, pathColumn } from '../columns';
import type { Column, KindDescriptor, Status } from '../types';

const ready = (o: K8sObject): string =>
  `${getNumber(o, 'status.readyReplicas')}/${getNumber(o, 'spec.replicas', 1)}`;

export function deploymentStatus(o: K8sObject): Status {
  if ((o as { spec?: { paused?: unknown } }).spec?.paused === true)
    return { text: 'Paused', severity: 'warning' };
  if (conditionStatus('Available')(o) === 'False') return { text: 'Degraded', severity: 'error' };
  const desired = getNumber(o, 'spec.replicas', 1);
  const updated = getNumber(o, 'status.updatedReplicas');
  const available = getNumber(o, 'status.availableReplicas');
  if (updated < desired || available < desired || getNumber(o, 'status.replicas') > desired) {
    return { text: 'Progressing', severity: 'warning' };
  }
  return conditionStatus('Available')(o) === 'True'
    ? { text: 'Available', severity: 'ok' }
    : { text: 'Progressing', severity: 'warning' };
}

export function statefulSetStatus(o: K8sObject): Status {
  const desired = getNumber(o, 'spec.replicas', 1);
  return getNumber(o, 'status.readyReplicas') >= desired
    ? { text: 'Ready', severity: 'ok' }
    : { text: 'Progressing', severity: 'warning' };
}

export function jobStatus(o: K8sObject): Status {
  if (conditionStatus('Complete')(o) === 'True') return { text: 'Complete', severity: 'ok' };
  if (conditionStatus('Failed')(o) === 'True') return { text: 'Failed', severity: 'error' };
  if ((o as { spec?: { suspend?: unknown } }).spec?.suspend === true)
    return { text: 'Suspended', severity: 'warning' };
  return { text: 'Running', severity: 'ok' };
}

function duration(o: K8sObject, now: number): string {
  const start = getString(o, 'status.startTime');
  if (!start) return '';
  const end = getString(o, 'status.completionTime');
  const endMs = end ? Date.parse(end) : now;
  return formatAge(start, endMs);
}

function owner(o: K8sObject): string {
  const ref = getArray(o, 'metadata.ownerReferences')[0];
  return ref ? `${getString(ref, 'kind')}/${getString(ref, 'name')}` : '';
}

const readyColumn = (value: (o: K8sObject) => string): Column => ({ id: 'ready', title: 'Ready', value });
const statusColumn = (fn: (o: K8sObject) => Status): Column => ({
  id: 'status',
  title: 'Status',
  value: (o) => fn(o).text,
});

export const deploymentsKind: KindDescriptor = defineKind({
  group: 'apps',
  kind: 'Deployment',
  resource: 'deployments',
  namespaced: true,
  title: 'Deployments',
  section: 'Workloads',
  status: deploymentStatus,
  actions: ['scale', 'restart', 'pause', 'resume', 'rollback', 'setImage', 'delete'],
  columns: [
    readyColumn(ready),
    numberColumn('upToDate', 'Up-to-date', 'status.updatedReplicas'),
    numberColumn('available', 'Available', 'status.availableReplicas'),
    statusColumn(deploymentStatus),
  ],
});

export const statefulSetsKind: KindDescriptor = defineKind({
  group: 'apps',
  kind: 'StatefulSet',
  resource: 'statefulsets',
  namespaced: true,
  title: 'StatefulSets',
  section: 'Workloads',
  status: statefulSetStatus,
  actions: ['scale', 'restart', 'setImage', 'delete'],
  columns: [readyColumn(ready), statusColumn(statefulSetStatus)],
});

export const daemonSetsKind: KindDescriptor = defineKind({
  group: 'apps',
  kind: 'DaemonSet',
  resource: 'daemonsets',
  namespaced: true,
  title: 'DaemonSets',
  section: 'Workloads',
  actions: ['restart', 'setImage', 'delete'],
  columns: [
    numberColumn('desired', 'Desired', 'status.desiredNumberScheduled'),
    numberColumn('current', 'Current', 'status.currentNumberScheduled'),
    numberColumn('ready', 'Ready', 'status.numberReady'),
    numberColumn('upToDate', 'Up-to-date', 'status.updatedNumberScheduled'),
    numberColumn('available', 'Available', 'status.numberAvailable'),
    {
      id: 'nodeSelector',
      title: 'Node selector',
      value: (o) =>
        labelString(
          (o as { spec?: { template?: { spec?: { nodeSelector?: unknown } } } }).spec?.template?.spec
            ?.nodeSelector,
        ),
    },
  ],
});

export const replicaSetsKind: KindDescriptor = defineKind({
  group: 'apps',
  kind: 'ReplicaSet',
  resource: 'replicasets',
  namespaced: true,
  title: 'ReplicaSets',
  section: 'Workloads',
  actions: ['scale', 'delete'],
  columns: [
    numberColumn('desired', 'Desired', 'spec.replicas'),
    numberColumn('current', 'Current', 'status.replicas'),
    numberColumn('ready', 'Ready', 'status.readyReplicas'),
    { id: 'owner', title: 'Owner', value: owner },
  ],
});

export const jobsKind: KindDescriptor = defineKind({
  group: 'batch',
  kind: 'Job',
  resource: 'jobs',
  namespaced: true,
  title: 'Jobs',
  section: 'Workloads',
  status: jobStatus,
  actions: ['delete'],
  columns: [
    {
      id: 'completions',
      title: 'Completions',
      value: (o) => {
        const spec = (o as { spec?: { completions?: unknown } }).spec;
        return `${getNumber(o, 'status.succeeded')}/${typeof spec?.completions === 'number' ? spec.completions : 1}`;
      },
    },
    { id: 'duration', title: 'Duration', value: duration },
    statusColumn(jobStatus),
  ],
});

export const cronJobsKind: KindDescriptor = defineKind({
  group: 'batch',
  kind: 'CronJob',
  resource: 'cronjobs',
  namespaced: true,
  title: 'CronJobs',
  section: 'Workloads',
  actions: ['cronjobTrigger', 'cronjobSuspend', 'cronjobResume', 'delete'],
  columns: [
    pathColumn('schedule', 'Schedule', 'spec.schedule'),
    {
      id: 'suspend',
      title: 'Suspend',
      value: (o) => ((o as { spec?: { suspend?: unknown } }).spec?.suspend === true ? 'true' : 'false'),
    },
    { id: 'active', title: 'Active', value: (o) => getArray(o, 'status.active').length, sort: 'number' },
    {
      id: 'lastSchedule',
      title: 'Last schedule',
      value: (o, now) => formatAge(getString(o, 'status.lastScheduleTime'), now),
      sort: 'age',
    },
  ],
});
