import { getArray, getString } from '../lib/path';
import type { K8sObject } from '../state/live';
import { conditionStatus, defineKind } from './columns';
import type { Column, KindDescriptor, Status } from './types';

type Via = 'spec' | 'annotation' | 'never';

export interface FluxKindDef {
  group: string;
  kind: string;
  /** The plural resource name. */
  resource: string;
  /** How the kind is suspended; the same table as `internal/flux/kinds.go`. */
  via: Via;
  annotation?: string;
}

const OPERATOR = 'fluxcd.controlplane.io/reconcile';
const GENERATOR = 'source.extensions.fluxcd.io/reconcile';

const def = (
  group: string,
  kind: string,
  resource: string,
  via: Via = 'spec',
  annotation?: string,
): FluxKindDef => ({
  group,
  kind,
  resource,
  via,
  ...(annotation && { annotation }),
});

/** The Flux kinds of the spec's table. */
export const FLUX_KINDS: FluxKindDef[] = [
  def('fluxcd.controlplane.io', 'FluxInstance', 'fluxinstances', 'annotation', OPERATOR),
  def('fluxcd.controlplane.io', 'FluxReport', 'fluxreports', 'never'),
  def('fluxcd.controlplane.io', 'ResourceSet', 'resourcesets', 'annotation', OPERATOR),
  def(
    'fluxcd.controlplane.io',
    'ResourceSetInputProvider',
    'resourcesetinputproviders',
    'annotation',
    OPERATOR,
  ),
  def('source.toolkit.fluxcd.io', 'GitRepository', 'gitrepositories'),
  def('source.toolkit.fluxcd.io', 'OCIRepository', 'ocirepositories'),
  def('source.toolkit.fluxcd.io', 'HelmRepository', 'helmrepositories'),
  def('source.toolkit.fluxcd.io', 'HelmChart', 'helmcharts'),
  def('source.toolkit.fluxcd.io', 'Bucket', 'buckets'),
  def('source.toolkit.fluxcd.io', 'ExternalArtifact', 'externalartifacts'),
  def('source.extensions.fluxcd.io', 'ArtifactGenerator', 'artifactgenerators', 'annotation', GENERATOR),
  def('kustomize.toolkit.fluxcd.io', 'Kustomization', 'kustomizations'),
  def('helm.toolkit.fluxcd.io', 'HelmRelease', 'helmreleases'),
  def('image.toolkit.fluxcd.io', 'ImageRepository', 'imagerepositories'),
  def('image.toolkit.fluxcd.io', 'ImagePolicy', 'imagepolicies'),
  def('image.toolkit.fluxcd.io', 'ImageUpdateAutomation', 'imageupdateautomations'),
  def('notification.toolkit.fluxcd.io', 'Provider', 'providers'),
  def('notification.toolkit.fluxcd.io', 'Alert', 'alerts'),
  def('notification.toolkit.fluxcd.io', 'Receiver', 'receivers'),
];

export function findFluxKind(group: string, kind: string): FluxKindDef | undefined {
  return FLUX_KINDS.find((k) => k.group === group && k.kind === kind);
}

/** Whether the object is suspended: `spec.suspend`, or the annotation of kinds that have no such field. */
export function isSuspended(kind: FluxKindDef, o: K8sObject): boolean {
  if (kind.via === 'spec') return (o.spec as { suspend?: unknown } | undefined)?.suspend === true;
  if (kind.via === 'annotation' && kind.annotation) {
    const annotations = (o.metadata as { annotations?: Record<string, unknown> } | undefined)?.annotations;
    const v = annotations?.[kind.annotation];
    return typeof v === 'string' && v.toLowerCase() === 'disabled';
  }
  return false;
}

/** The Ready condition decides: True → Ready, False → Failed, Unknown or Reconciling=True → Reconciling. */
export function fluxStatus(o: K8sObject): Status {
  if (conditionStatus('Reconciling')(o) === 'True') return { text: 'Reconciling', severity: 'warning' };
  switch (conditionStatus('Ready')(o)) {
    case 'True':
      return { text: 'Ready', severity: 'ok' };
    case 'False':
      return { text: 'Failed', severity: 'error' };
    case 'Unknown':
      return { text: 'Reconciling', severity: 'warning' };
    default:
      return { text: 'Unknown', severity: 'unknown' };
  }
}

/** The revision a kind last applied or published. */
export function revisionOf(o: K8sObject): string {
  return (
    getString(o, 'status.lastAppliedRevision') ||
    getString(o, 'status.artifact.revision') ||
    getString(o, 'status.lastAttemptedRevision')
  );
}

function readyMessage(o: K8sObject): string {
  const c = getArray(o, 'status.conditions').find((x) => getString(x, 'type') === 'Ready');
  return c ? getString(c, 'message') : '';
}

/** The descriptor of every Flux kind: lists and the sidebar read it. */
export function fluxDescriptors(): KindDescriptor[] {
  return FLUX_KINDS.map((k) => {
    const columns: Column[] = [
      { id: 'ready', title: 'Ready', value: (o) => conditionStatus('Ready')(o) },
      { id: 'status', title: 'Status', value: (o) => readyMessage(o) },
      { id: 'revision', title: 'Revision', value: (o) => revisionOf(o) },
      { id: 'suspended', title: 'Suspended', value: (o) => (isSuspended(k, o) ? 'Yes' : '') },
    ];
    return defineKind({
      group: k.group,
      kind: k.kind,
      resource: k.resource,
      namespaced: true,
      title: k.kind,
      section: 'FluxCD',
      columns,
      status: (o) => (isSuspended(k, o) ? { text: 'Suspended', severity: 'warning' } : fluxStatus(o)),
    });
  });
}
