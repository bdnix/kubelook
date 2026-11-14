import { get, getArray, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { defineKind, pathColumn } from '../columns';
import type { KindDescriptor } from '../types';

const ACCESS_MODES: Record<string, string> = {
  ReadWriteOnce: 'RWO',
  ReadOnlyMany: 'ROX',
  ReadWriteMany: 'RWX',
  ReadWriteOncePod: 'RWOP',
};

function modes(o: K8sObject, path: string): string {
  return getArray(o, path)
    .filter((m): m is string => typeof m === 'string')
    .map((m) => ACCESS_MODES[m] ?? m)
    .join(',');
}

function claim(o: K8sObject): string {
  const ns = getString(o, 'spec.claimRef.namespace');
  const name = getString(o, 'spec.claimRef.name');
  return name ? `${ns}/${name}` : '';
}

export const pvcsKind: KindDescriptor = defineKind({
  group: '',
  kind: 'PersistentVolumeClaim',
  resource: 'persistentvolumeclaims',
  namespaced: true,
  title: 'PersistentVolumeClaims',
  section: 'Storage',
  actions: ['expandPVC', 'delete'],
  status: (o) => {
    const text = getString(o, 'status.phase');
    return {
      text,
      severity:
        text === 'Bound' ? 'ok' : text === 'Pending' ? 'warning' : text === 'Lost' ? 'error' : 'unknown',
    };
  },
  columns: [
    pathColumn('status', 'Status', 'status.phase'),
    pathColumn('volume', 'Volume', 'spec.volumeName'),
    pathColumn('capacity', 'Capacity', 'status.capacity.storage'),
    { id: 'accessModes', title: 'Access modes', value: (o) => modes(o, 'spec.accessModes') },
    pathColumn('storageClass', 'Storage class', 'spec.storageClassName'),
  ],
});

export const pvsKind: KindDescriptor = defineKind({
  group: '',
  kind: 'PersistentVolume',
  resource: 'persistentvolumes',
  namespaced: false,
  title: 'PersistentVolumes',
  section: 'Storage',
  actions: ['delete'],
  columns: [
    pathColumn('capacity', 'Capacity', 'spec.capacity.storage'),
    { id: 'accessModes', title: 'Access modes', value: (o) => modes(o, 'spec.accessModes') },
    pathColumn('reclaimPolicy', 'Reclaim policy', 'spec.persistentVolumeReclaimPolicy'),
    pathColumn('status', 'Status', 'status.phase'),
    { id: 'claim', title: 'Claim', value: claim },
    pathColumn('storageClass', 'Storage class', 'spec.storageClassName'),
  ],
});

export const storageClassesKind: KindDescriptor = defineKind({
  group: 'storage.k8s.io',
  kind: 'StorageClass',
  resource: 'storageclasses',
  namespaced: false,
  title: 'StorageClasses',
  section: 'Storage',
  actions: ['delete'],
  columns: [
    pathColumn('provisioner', 'Provisioner', 'provisioner'),
    { id: 'reclaimPolicy', title: 'Reclaim policy', value: (o) => getString(o, 'reclaimPolicy') || 'Delete' },
    {
      id: 'bindingMode',
      title: 'Binding mode',
      value: (o) => getString(o, 'volumeBindingMode') || 'Immediate',
    },
    {
      id: 'expansion',
      title: 'Expansion',
      value: (o) => (get(o, 'allowVolumeExpansion') === true ? 'true' : 'false'),
    },
  ],
});

export const volumeSnapshotsKind: KindDescriptor = defineKind({
  group: 'snapshot.storage.k8s.io',
  kind: 'VolumeSnapshot',
  resource: 'volumesnapshots',
  namespaced: true,
  title: 'VolumeSnapshots',
  section: 'Storage',
  actions: ['delete'],
  columns: [
    pathColumn('ready', 'Ready', 'status.readyToUse'),
    pathColumn('sourcePVC', 'Source PVC', 'spec.source.persistentVolumeClaimName'),
    pathColumn('restoreSize', 'Restore size', 'status.restoreSize'),
    pathColumn('snapshotClass', 'Snapshot class', 'spec.volumeSnapshotClassName'),
  ],
});
