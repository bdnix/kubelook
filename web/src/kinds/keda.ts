import { getArray, getString } from '../lib/path';
import { conditionStatus, defineKind, numberColumn, pathColumn } from './columns';
import type { K8sObject } from '../state/live';
import type { Column, KindDescriptor } from './types';

const cond = (id: string, title: string, type: string): Column => ({
  id,
  title,
  value: (o) => conditionStatus(type)(o),
});
const joined =
  (path: string, field: string): ((o: K8sObject) => string) =>
  (o) =>
    getArray(o, path)
      .map((t) => getString(t, field))
      .filter((v) => v !== '')
      .join(',');

/** The KEDA kinds the console shows: ScaledObjects, with pause and resume. */
export function kedaDescriptors(): KindDescriptor[] {
  return [
    {
      ...defineKind({
        group: 'keda.sh',
        kind: 'ScaledObject',
        resource: 'scaledobjects',
        namespaced: true,
        title: 'ScaledObjects',
        section: 'Modules',
        columns: [
          pathColumn('targetkind', 'ScaleTargetKind', 'spec.scaleTargetRef.kind'),
          pathColumn('targetname', 'ScaleTargetName', 'spec.scaleTargetRef.name'),
          numberColumn('min', 'Min', 'spec.minReplicaCount'),
          numberColumn('max', 'Max', 'spec.maxReplicaCount'),
          cond('ready', 'Ready', 'Ready'),
          cond('active', 'Active', 'Active'),
          cond('fallback', 'Fallback', 'Fallback'),
          cond('paused', 'Paused', 'Paused'),
          { id: 'triggers', title: 'Triggers', value: joined('spec.triggers', 'type') },
          {
            id: 'authentications',
            title: 'Authentications',
            value: (o) =>
              getArray(o, 'spec.triggers')
                .map((t) => getString(t, 'authenticationRef.name'))
                .filter((v) => v !== '')
                .join(','),
          },
        ],
      }),
      actions: ['kedaPause', 'kedaResume'],
    },
  ];
}
