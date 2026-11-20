import { describe, expect, it } from 'vitest';
import { FLUX_KINDS, fluxDescriptors, fluxStatus, isSuspended, findFluxKind } from './flux';

const ready = (status: string, extra: Record<string, unknown>[] = []) => ({
  status: { conditions: [{ type: 'Ready', status, message: 'build failed', reason: 'BuildFailed' }, ...extra] },
});

describe('FLUX_KINDS', () => {
  it('are the kinds of the spec table', () => {
    expect(FLUX_KINDS).toHaveLength(19);
    expect(findFluxKind('kustomize.toolkit.fluxcd.io', 'Kustomization')?.hasStatus).toBe(true);
    expect(findFluxKind('notification.toolkit.fluxcd.io', 'Provider')?.hasStatus).toBe(true);
    expect(findFluxKind('keda.sh', 'ScaledObject')).toBeUndefined();
  });
});

describe('isSuspended', () => {
  it('reads spec.suspend or the annotation of the kind', () => {
    const ks = findFluxKind('kustomize.toolkit.fluxcd.io', 'Kustomization');
    expect(ks && isSuspended(ks, { spec: { suspend: true } })).toBe(true);
    expect(ks && isSuspended(ks, { spec: { suspend: false } })).toBe(false);
    const rs = findFluxKind('fluxcd.controlplane.io', 'ResourceSet');
    const off = { metadata: { annotations: { 'fluxcd.controlplane.io/reconcile': 'Disabled' } } };
    expect(rs && isSuspended(rs, off)).toBe(true);
    expect(rs && isSuspended(rs, {})).toBe(false);
    const gen = findFluxKind('source.extensions.fluxcd.io', 'ArtifactGenerator');
    expect(gen && isSuspended(gen, { metadata: { annotations: { 'source.extensions.fluxcd.io/reconcile': 'disabled' } } })).toBe(true);
    const report = findFluxKind('fluxcd.controlplane.io', 'FluxReport');
    expect(report && isSuspended(report, { spec: { suspend: true } })).toBe(false);
  });
});

describe('fluxStatus', () => {
  it('maps the Ready condition', () => {
    expect(fluxStatus(ready('True'))).toEqual({ text: 'Ready', severity: 'ok' });
    expect(fluxStatus(ready('False'))).toEqual({ text: 'Failed', severity: 'error' });
    expect(fluxStatus(ready('Unknown'))).toEqual({ text: 'Reconciling', severity: 'warning' });
    expect(fluxStatus(ready('True', [{ type: 'Reconciling', status: 'True' }]))).toEqual({ text: 'Reconciling', severity: 'warning' });
    expect(fluxStatus({})).toEqual({ text: 'Unknown', severity: 'unknown' });
  });
});

describe('fluxDescriptors', () => {
  const kustomization = fluxDescriptors().find((d) => d.kind === 'Kustomization');

  it('has a descriptor per kind, in the FluxCD section', () => {
    expect(fluxDescriptors()).toHaveLength(19);
    expect(kustomization?.section).toBe('FluxCD');
    expect(kustomization?.columns.map((c) => c.title)).toEqual(['Name', 'Namespace', 'Ready', 'Status', 'Revision', 'Suspended', 'Age']);
  });

  it('fills the columns from the object', () => {
    const o = { metadata: { name: 'apps', namespace: 'flux-system' }, spec: { suspend: true }, status: { lastAppliedRevision: 'main@sha1:abc', conditions: [{ type: 'Ready', status: 'False', message: 'build failed' }] } };
    const value = (id: string) => kustomization?.columns.find((c) => c.id === id)?.value(o, 0);
    expect(value('ready')).toBe('False');
    expect(value('status')).toBe('build failed');
    expect(value('revision')).toBe('main@sha1:abc');
    expect(value('suspended')).toBe('Yes');
    expect(kustomization?.status?.(o).severity).toBe('warning');
    expect(kustomization?.status?.({ ...o, spec: {} }).severity).toBe('error');
  });

  it('reads the revision of sources from the artifact', () => {
    const git = fluxDescriptors().find((d) => d.kind === 'GitRepository');
    const o = { status: { artifact: { revision: 'main@sha1:def' } } };
    expect(git?.columns.find((c) => c.id === 'revision')?.value(o, 0)).toBe('main@sha1:def');
  });

  it('names the cluster-scoped kinds (none) and the plural resources', () => {
    expect(fluxDescriptors().every((d) => d.namespaced)).toBe(true);
    expect(fluxDescriptors().find((d) => d.kind === 'ImagePolicy')?.resource).toBe('imagepolicies');
  });
});
