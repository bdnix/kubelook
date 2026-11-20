import type { Discovery, DiscoveryResource } from '../../api/discovery';

/** The API groups that Kubernetes itself serves; everything else is a custom resource. */
const BUILT_IN_GROUPS = new Set([
  '',
  'apps',
  'batch',
  'autoscaling',
  'policy',
  'admissionregistration.k8s.io',
  'apiextensions.k8s.io',
  'apiregistration.k8s.io',
  'authentication.k8s.io',
  'authorization.k8s.io',
  'certificates.k8s.io',
  'coordination.k8s.io',
  'discovery.k8s.io',
  'events.k8s.io',
  'flowcontrol.apiserver.k8s.io',
  'internal.apiserver.k8s.io',
  'networking.k8s.io',
  'node.k8s.io',
  'rbac.authorization.k8s.io',
  'resource.k8s.io',
  'scheduling.k8s.io',
  'storage.k8s.io',
  'metrics.k8s.io',
  'custom.metrics.k8s.io',
  'external.metrics.k8s.io',
]);

/**
 * The kinds the Custom Resources section lists: served, listable, not built into Kubernetes and not claimed by
 * an active module (Step 5.2), sorted by group and kind.
 */
export function customKinds(
  discovery: Discovery,
  claimed: (group: string, kind: string) => boolean = () => false,
): DiscoveryResource[] {
  return discovery.resources
    .filter(
      (r) =>
        r.preferred && r.verbs.includes('list') && !BUILT_IN_GROUPS.has(r.group) && !claimed(r.group, r.kind),
    )
    .sort((a, b) => a.group.localeCompare(b.group) || a.kind.localeCompare(b.kind));
}

/** The text "Create from YAML" opens with for a kind: apiVersion, kind and a name (and a namespace). */
export function kindTemplate(
  r: Pick<DiscoveryResource, 'group' | 'version' | 'kind' | 'namespaced'>,
): string {
  const apiVersion = r.group === '' ? r.version : `${r.group}/${r.version}`;
  return `apiVersion: ${apiVersion}\nkind: ${r.kind}\nmetadata:\n  name: example\n${r.namespaced ? '  namespace: default\n' : ''}`;
}
