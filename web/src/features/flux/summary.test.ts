import { describe, expect, it } from 'vitest';
import { findFluxKind } from '../../kinds/flux';
import { controllerHealth, servedFluxKinds, summarize } from './summary';

const ks = findFluxKind('kustomize.toolkit.fluxcd.io', 'Kustomization');
const rs = findFluxKind('fluxcd.controlplane.io', 'ResourceSet');
const alert = findFluxKind('notification.toolkit.fluxcd.io', 'Alert');

const cond = (type: string, status: string) => ({ type, status });
const obj = (conditions: unknown[], extra: Record<string, unknown> = {}) => ({
  status: { conditions },
  ...extra,
});

describe('summarize', () => {
  it('counts ready, failed, in progress and suspended objects, each object once', () => {
    if (!ks) throw new Error('no kind');
    const s = summarize(ks, [
      obj([cond('Ready', 'True')]),
      obj([cond('Ready', 'True')]),
      obj([cond('Ready', 'False')]),
      obj([cond('Ready', 'Unknown')]),
      obj([cond('Ready', 'True'), cond('Reconciling', 'True')]),
      obj([cond('Ready', 'False')], { spec: { suspend: true } }),
    ]);
    expect(s).toEqual({ total: 6, reportsStatus: true, ready: 2, failed: 1, inProgress: 2, suspended: 1 });
  });

  it('counts a kind that is suspended by an annotation', () => {
    if (!rs) throw new Error('no kind');
    const s = summarize(rs, [
      obj([cond('Ready', 'True')], {
        metadata: { annotations: { 'fluxcd.controlplane.io/reconcile': 'disabled' } },
      }),
    ]);
    expect(s.suspended).toBe(1);
    expect(s.ready).toBe(0);
  });

  it('counts only the total and the suspended objects of a kind that reports no status', () => {
    if (!alert) throw new Error('no kind');
    const s = summarize(alert, [{ spec: {} }, { spec: { suspend: true } }, { spec: {} }]);
    expect(s).toEqual({ total: 3, reportsStatus: false, ready: 0, failed: 0, inProgress: 0, suspended: 1 });
  });

  it('is all zeros for no objects', () => {
    if (!ks) throw new Error('no kind');
    expect(summarize(ks, [])).toEqual({
      total: 0,
      reportsStatus: false,
      ready: 0,
      failed: 0,
      inProgress: 0,
      suspended: 0,
    });
  });
});

describe('servedFluxKinds', () => {
  it('keeps the Flux kinds the cluster serves and hides the others', () => {
    const served = servedFluxKinds({
      serverVersion: 'v1.35.0',
      resources: [
        {
          group: 'kustomize.toolkit.fluxcd.io',
          version: 'v1',
          resource: 'kustomizations',
          kind: 'Kustomization',
          namespaced: true,
          verbs: ['list'],
          preferred: true,
          servedVersions: ['v1'],
        },
        {
          group: 'keda.sh',
          version: 'v1',
          resource: 'scaledobjects',
          kind: 'ScaledObject',
          namespaced: true,
          verbs: ['list'],
          preferred: true,
          servedVersions: ['v1'],
        },
      ],
    });
    expect(served.map((s) => s.def.kind)).toEqual(['Kustomization']);
    expect(served[0]?.version).toBe('v1');
    expect(served[0]?.resource).toBe('kustomizations');
  });
});

describe('controllerHealth', () => {
  const dep = (name: string, labels: Record<string, string>, desired: number, available: number) => ({
    metadata: { name, labels },
    spec: { replicas: desired },
    status: { availableReplicas: available },
  });

  it('lists the Flux controllers with their version and whether they are available', () => {
    const got = controllerHealth([
      dep(
        'source-controller',
        { 'app.kubernetes.io/part-of': 'flux', 'app.kubernetes.io/version': 'v2.7.0' },
        1,
        1,
      ),
      dep(
        'kustomize-controller',
        { 'app.kubernetes.io/part-of': 'flux', 'app.kubernetes.io/version': 'v2.7.0' },
        2,
        1,
      ),
      dep('web', { app: 'web' }, 1, 1),
    ]);
    expect(got).toEqual([
      { name: 'kustomize-controller', version: 'v2.7.0', ready: false, desired: 2, available: 1 },
      { name: 'source-controller', version: 'v2.7.0', ready: true, desired: 1, available: 1 },
    ]);
  });

  it('is empty without Flux controllers', () => {
    expect(controllerHealth([])).toEqual([]);
  });
});
