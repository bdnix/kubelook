import { get } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { countColumn, countOf, defineKind, numberColumn, pathColumn } from '../columns';
import type { KindDescriptor } from '../types';

function summary(o: K8sObject): string {
  const hard = get(o, 'status.hard');
  const used = get(o, 'status.used');
  if (hard === null || typeof hard !== 'object') return '';
  return Object.keys(hard as object)
    .sort()
    .map(
      (k) =>
        `${k}: ${String((used as Record<string, unknown> | undefined)?.[k] ?? '0')}/${String((hard as Record<string, unknown>)[k])}`,
    )
    .join(', ');
}

function scalar(o: K8sObject, path: string): string {
  const v = get(o, path);
  return typeof v === 'string' || typeof v === 'number' ? String(v) : '';
}

function hpaStatus(o: K8sObject): string {
  const c = (get(o, 'status.conditions') as unknown[] | undefined)?.find(
    (x) => get(x, 'type') === 'ScalingActive',
  );
  const s = c ? get(c, 'status') : undefined;
  return s === 'True' ? 'Active' : s === 'False' ? 'Inactive' : 'Unknown';
}

export const configMapsKind: KindDescriptor = defineKind({
  group: '',
  kind: 'ConfigMap',
  resource: 'configmaps',
  namespaced: true,
  title: 'ConfigMaps',
  section: 'Config',
  actions: ['delete'],
  columns: [
    {
      id: 'keys',
      title: 'Keys',
      value: (o) => countOf(o, 'data') + countOf(o, 'binaryData'),
      sort: 'number',
    },
  ],
});

export const secretsKind: KindDescriptor = defineKind({
  group: '',
  kind: 'Secret',
  resource: 'secrets',
  namespaced: true,
  title: 'Secrets',
  section: 'Config',
  actions: ['delete'],
  columns: [pathColumn('type', 'Type', 'type'), countColumn('keys', 'Keys', 'data')],
});

export const resourceQuotasKind: KindDescriptor = defineKind({
  group: '',
  kind: 'ResourceQuota',
  resource: 'resourcequotas',
  namespaced: true,
  title: 'ResourceQuotas',
  section: 'Config',
  actions: ['delete'],
  columns: [{ id: 'usage', title: 'Used/hard', value: summary }],
});

export const limitRangesKind: KindDescriptor = defineKind({
  group: '',
  kind: 'LimitRange',
  resource: 'limitranges',
  namespaced: true,
  title: 'LimitRanges',
  section: 'Config',
  actions: ['delete'],
  columns: [],
});

export const hpaKind: KindDescriptor = defineKind({
  group: 'autoscaling',
  kind: 'HorizontalPodAutoscaler',
  resource: 'horizontalpodautoscalers',
  namespaced: true,
  title: 'HorizontalPodAutoscalers',
  section: 'Config',
  actions: ['delete'],
  columns: [
    {
      id: 'reference',
      title: 'Reference',
      value: (o) =>
        `${scalar(o, 'spec.scaleTargetRef.kind')}/${scalar(o, 'spec.scaleTargetRef.name')}`.replace(
          /^\/$/,
          '',
        ),
    },
    { id: 'min', title: 'Min', value: (o) => scalar(o, 'spec.minReplicas') || '1', sort: 'number' },
    numberColumn('max', 'Max', 'spec.maxReplicas'),
    numberColumn('replicas', 'Replicas', 'status.currentReplicas'),
    { id: 'status', title: 'Status', value: hpaStatus },
  ],
});

export const pdbKind: KindDescriptor = defineKind({
  group: 'policy',
  kind: 'PodDisruptionBudget',
  resource: 'poddisruptionbudgets',
  namespaced: true,
  title: 'PodDisruptionBudgets',
  section: 'Config',
  actions: ['delete'],
  columns: [
    { id: 'minAvailable', title: 'Min available', value: (o) => scalar(o, 'spec.minAvailable') },
    { id: 'maxUnavailable', title: 'Max unavailable', value: (o) => scalar(o, 'spec.maxUnavailable') },
    numberColumn('allowed', 'Allowed disruptions', 'status.disruptionsAllowed'),
  ],
});

export const priorityClassesKind: KindDescriptor = defineKind({
  group: 'scheduling.k8s.io',
  kind: 'PriorityClass',
  resource: 'priorityclasses',
  namespaced: false,
  title: 'PriorityClasses',
  section: 'Config',
  actions: ['delete'],
  columns: [
    numberColumn('value', 'Value', 'value'),
    {
      id: 'globalDefault',
      title: 'Global default',
      value: (o) => (get(o, 'globalDefault') === true ? 'true' : 'false'),
    },
  ],
});
