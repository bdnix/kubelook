import { describe, expect, it } from 'vitest';
import { findConsumers } from './consumers';

const workload = (kind: string, name: string, podSpec: Record<string, unknown>, namespace = 'ns') => ({
  kind,
  metadata: { name, namespace },
  spec: { template: { spec: podSpec } },
});

describe('findConsumers', () => {
  const ws = [
    workload('Deployment', 'envfrom', {
      containers: [{ name: 'a', envFrom: [{ secretRef: { name: 'db' } }] }],
    }),
    workload('Deployment', 'key', {
      containers: [
        { name: 'a', env: [{ name: 'X', valueFrom: { secretKeyRef: { name: 'db', key: 'k' } } }] },
      ],
    }),
    workload('StatefulSet', 'volume', {
      containers: [{ name: 'a' }],
      volumes: [{ name: 'v', secret: { secretName: 'db' } }],
    }),
    workload('DaemonSet', 'init', {
      containers: [{ name: 'a' }],
      initContainers: [{ name: 'i', envFrom: [{ secretRef: { name: 'db' } }] }],
    }),
    workload('Deployment', 'other', {
      containers: [{ name: 'a', envFrom: [{ secretRef: { name: 'other' } }] }],
    }),
    workload('Deployment', 'configmap', {
      containers: [{ name: 'a', envFrom: [{ configMapRef: { name: 'db' } }] }],
    }),
    workload(
      'Deployment',
      'elsewhere',
      { containers: [{ name: 'a', envFrom: [{ secretRef: { name: 'db' } }] }] },
      'other-ns',
    ),
  ];
  it('finds the workloads that use the secret through envFrom, secretKeyRef or a volume, in its namespace', () => {
    expect(findConsumers('db', 'ns', ws).map((c) => `${c.kind}/${c.name}`)).toEqual([
      'Deployment/envfrom',
      'Deployment/key',
      'StatefulSet/volume',
      'DaemonSet/init',
    ]);
  });
  it('is empty for a secret nobody uses', () => {
    expect(findConsumers('unused', 'ns', ws)).toEqual([]);
  });
  it('ignores a workload without a pod template', () => {
    expect(
      findConsumers('db', 'ns', [{ kind: 'Deployment', metadata: { name: 'x', namespace: 'ns' } }]),
    ).toEqual([]);
  });
});
