import { describe, expect, it } from 'vitest';
import { expectToleratesMissingFields, meta, row } from '../testing';
import { pvcsKind, pvsKind, storageClassesKind, volumeSnapshotsKind } from './storage';

describe('storage kinds', () => {
  it('PVC shows phase, volume, capacity, short access modes and the class', () => {
    const pvc = (phase: string) => ({
      metadata: meta('data', 'ns'),
      spec: {
        volumeName: 'pv-1',
        accessModes: ['ReadWriteOnce', 'ReadOnlyMany', 'ReadWriteMany', 'ReadWriteOncePod', 'Other'],
        storageClassName: 'fast',
      },
      status: { phase, capacity: { storage: '10Gi' } },
    });
    expect(row(pvcsKind, pvc('Bound'))).toMatchObject({
      status: 'Bound',
      volume: 'pv-1',
      capacity: '10Gi',
      accessModes: 'RWO,ROX,RWX,RWOP,Other',
      storageClass: 'fast',
    });
    expect(pvcsKind.status?.(pvc('Bound'))).toEqual({ text: 'Bound', severity: 'ok' });
    expect(pvcsKind.status?.(pvc('Pending')).severity).toBe('warning');
    expect(pvcsKind.status?.(pvc('Lost')).severity).toBe('error');
    expect(pvcsKind.status?.(pvc('Other')).severity).toBe('unknown');
    expectToleratesMissingFields(pvcsKind);
  });

  it('PV shows capacity, modes, reclaim policy, status, claim and class', () => {
    const pv = {
      metadata: meta('pv-1'),
      spec: {
        capacity: { storage: '5Gi' },
        accessModes: ['ReadWriteOnce'],
        persistentVolumeReclaimPolicy: 'Retain',
        claimRef: { namespace: 'ns', name: 'data' },
        storageClassName: 'fast',
      },
      status: { phase: 'Bound' },
    };
    expect(row(pvsKind, pv)).toMatchObject({
      capacity: '5Gi',
      accessModes: 'RWO',
      reclaimPolicy: 'Retain',
      status: 'Bound',
      claim: 'ns/data',
      storageClass: 'fast',
    });
    expect(row(pvsKind, { metadata: meta('pv-2'), spec: {} }).claim).toBe('');
    expectToleratesMissingFields(pvsKind);
  });

  it('StorageClass uses the Kubernetes defaults for reclaim policy and binding mode', () => {
    expect(row(storageClassesKind, { metadata: meta('sc'), provisioner: 'csi.example' })).toMatchObject({
      provisioner: 'csi.example',
      reclaimPolicy: 'Delete',
      bindingMode: 'Immediate',
      expansion: 'false',
    });
    expect(
      row(storageClassesKind, {
        metadata: meta('sc'),
        reclaimPolicy: 'Retain',
        volumeBindingMode: 'WaitForFirstConsumer',
        allowVolumeExpansion: true,
      }),
    ).toMatchObject({ reclaimPolicy: 'Retain', bindingMode: 'WaitForFirstConsumer', expansion: 'true' });
    expectToleratesMissingFields(storageClassesKind);
  });

  it('VolumeSnapshot shows readiness, source, size and class', () => {
    const vs = {
      metadata: meta('snap', 'ns'),
      spec: { source: { persistentVolumeClaimName: 'data' }, volumeSnapshotClassName: 'csi' },
      status: { readyToUse: true, restoreSize: '10Gi' },
    };
    expect(row(volumeSnapshotsKind, vs)).toMatchObject({
      ready: 'true',
      sourcePVC: 'data',
      restoreSize: '10Gi',
      snapshotClass: 'csi',
    });
    expectToleratesMissingFields(volumeSnapshotsKind);
  });
});
