import { formatAge } from '../../lib/age';
import { formatBytes, formatCores, parseQuantity } from '../../lib/quantity';
import { get, getArray, getNumber, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { conditionStatus, countColumn, defineKind, pathColumn } from '../columns';
import type { KindDescriptor, Status } from '../types';

const PRESSURE = ['MemoryPressure', 'DiskPressure', 'PIDPressure', 'NetworkUnavailable'];

export function nodeStatus(o: K8sObject): Status {
  if (get(o, 'spec.unschedulable') === true) return { text: 'SchedulingDisabled', severity: 'warning' };
  return conditionStatus('Ready')(o) === 'True'
    ? { text: 'Ready', severity: 'ok' }
    : { text: 'NotReady', severity: 'error' };
}

function roles(o: K8sObject): string {
  const labels = get(o, 'metadata.labels');
  if (labels === null || typeof labels !== 'object') return '';
  const prefix = 'node-role.kubernetes.io/';
  return Object.keys(labels)
    .filter((k) => k.startsWith(prefix))
    .map((k) => k.slice(prefix.length))
    .sort()
    .join(',');
}

function internalIP(o: K8sObject): string {
  const a = getArray(o, 'status.addresses').find((x) => getString(x, 'type') === 'InternalIP');
  return a ? getString(a, 'address') : '';
}

function pressure(o: K8sObject): string {
  return PRESSURE.filter((t) => conditionStatus(t)(o) === 'True').join(',');
}

function allocatable(o: K8sObject, key: string, format: (n: number) => string): string {
  const q = getString(o, `status.allocatable.${key}`);
  return q ? format(parseQuantity(q)) : '';
}

function eventTime(o: K8sObject): string {
  return (
    getString(o, 'series.lastObservedTime') ||
    getString(o, 'eventTime') ||
    getString(o, 'deprecatedLastTimestamp') ||
    getString(o, 'metadata.creationTimestamp')
  );
}

export function eventStatus(o: K8sObject): Status {
  return getString(o, 'type') === 'Warning'
    ? { text: 'Warning', severity: 'warning' }
    : { text: 'Normal', severity: 'ok' };
}

export const nodesKind: KindDescriptor = defineKind({
  group: '',
  kind: 'Node',
  resource: 'nodes',
  namespaced: false,
  title: 'Nodes',
  section: 'Cluster',
  status: nodeStatus,
  actions: ['cordon', 'uncordon', 'drain', 'delete'],
  columns: [
    { id: 'status', title: 'Status', value: (o) => nodeStatus(o).text },
    { id: 'roles', title: 'Roles', value: roles },
    pathColumn('version', 'Version', 'status.nodeInfo.kubeletVersion'),
    { id: 'internalIP', title: 'Internal IP', value: internalIP },
    { id: 'cpu', title: 'CPU', value: (o) => allocatable(o, 'cpu', formatCores) },
    { id: 'memory', title: 'Memory', value: (o) => allocatable(o, 'memory', formatBytes) },
    { id: 'pressure', title: 'Pressure', value: pressure },
  ],
});

export const namespacesKind: KindDescriptor = defineKind({
  group: '',
  kind: 'Namespace',
  resource: 'namespaces',
  namespaced: false,
  title: 'Namespaces',
  section: 'Cluster',
  actions: ['delete'],
  columns: [pathColumn('status', 'Status', 'status.phase')],
});

export const eventsKind: KindDescriptor = defineKind({
  group: 'events.k8s.io',
  kind: 'Event',
  resource: 'events',
  namespaced: true,
  title: 'Events',
  section: 'Events',
  status: eventStatus,
  columns: [
    pathColumn('type', 'Type', 'type'),
    pathColumn('reason', 'Reason', 'reason'),
    {
      id: 'object',
      title: 'Object',
      value: (o) => {
        const kind = getString(o, 'regarding.kind');
        return kind ? `${kind}/${getString(o, 'regarding.name')}` : '';
      },
    },
    pathColumn('message', 'Message', 'note'),
    {
      id: 'count',
      title: 'Count',
      value: (o) => getNumber(o, 'series.count') || getNumber(o, 'deprecatedCount', 1),
      sort: 'number',
    },
    { id: 'lastSeen', title: 'Last seen', value: (o, now) => formatAge(eventTime(o), now), sort: 'age' },
  ],
});

export const leasesKind: KindDescriptor = defineKind({
  group: 'coordination.k8s.io',
  kind: 'Lease',
  resource: 'leases',
  namespaced: true,
  title: 'Leases',
  section: 'Cluster',
  columns: [
    pathColumn('holder', 'Holder', 'spec.holderIdentity'),
    {
      id: 'renewTime',
      title: 'Renew time',
      value: (o, now) => formatAge(getString(o, 'spec.renewTime'), now),
      sort: 'age',
    },
  ],
});

export const mutatingWebhooksKind: KindDescriptor = defineKind({
  group: 'admissionregistration.k8s.io',
  kind: 'MutatingWebhookConfiguration',
  resource: 'mutatingwebhookconfigurations',
  namespaced: false,
  title: 'MutatingWebhookConfigurations',
  section: 'Cluster',
  actions: ['delete'],
  columns: [countColumn('webhooks', 'Webhooks', 'webhooks')],
});

export const validatingWebhooksKind: KindDescriptor = defineKind({
  group: 'admissionregistration.k8s.io',
  kind: 'ValidatingWebhookConfiguration',
  resource: 'validatingwebhookconfigurations',
  namespaced: false,
  title: 'ValidatingWebhookConfigurations',
  section: 'Cluster',
  actions: ['delete'],
  columns: [countColumn('webhooks', 'Webhooks', 'webhooks')],
});
