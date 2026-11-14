import { getArray, getString } from './path';
import { matchSelector, type LabelSelector } from './selectors';
import type { K8sObject } from '../state/live';

const uidOf = (o: K8sObject) => getString(o, 'metadata.uid');

/** Objects whose `ownerReferences` point at `uid`. */
export function childrenOf(uid: string, candidates: K8sObject[]): K8sObject[] {
  if (uid === '') return [];
  return candidates.filter((c) =>
    getArray(c, 'metadata.ownerReferences').some((r) => getString(r, 'uid') === uid),
  );
}

/** Pods a Service routes to: those in its namespace whose labels match its selector. Without a selector, none. */
export function podsForService(service: K8sObject, pods: K8sObject[]): K8sObject[] {
  const selector = (service as { spec?: { selector?: Record<string, string> } }).spec?.selector;
  if (!selector || Object.keys(selector).length === 0) return [];
  const ns = getString(service, 'metadata.namespace');
  return pods.filter(
    (p) =>
      getString(p, 'metadata.namespace') === ns &&
      matchSelector(
        { matchLabels: selector } satisfies LabelSelector,
        (p as { metadata?: { labels?: Record<string, string> } }).metadata?.labels,
      ),
  );
}

/** The Services an Ingress sends traffic to, by name (default backend and every rule path). */
export function servicesOfIngress(ingress: K8sObject): string[] {
  const names = new Set<string>();
  const add = (n: string) => {
    if (n !== '') names.add(n);
  };
  add(getString(ingress, 'spec.defaultBackend.service.name'));
  for (const rule of getArray(ingress, 'spec.rules')) {
    for (const path of getArray(rule, 'http.paths')) add(getString(path, 'backend.service.name'));
  }
  return [...names].sort();
}

/** Pods that mount the PersistentVolumeClaim, in the claim's namespace. */
export function podsMountingClaim(claim: K8sObject, pods: K8sObject[]): K8sObject[] {
  const name = getString(claim, 'metadata.name');
  const ns = getString(claim, 'metadata.namespace');
  return pods.filter(
    (p) =>
      getString(p, 'metadata.namespace') === ns &&
      getArray(p, 'spec.volumes').some((v) => getString(v, 'persistentVolumeClaim.claimName') === name),
  );
}

/** Pods selected by a workload's `spec.selector`. */
export function podsOfWorkload(workload: K8sObject, pods: K8sObject[]): K8sObject[] {
  const selector = (workload as { spec?: { selector?: LabelSelector } }).spec?.selector;
  const ns = getString(workload, 'metadata.namespace');
  if (!selector) return [];
  return pods.filter(
    (p) =>
      getString(p, 'metadata.namespace') === ns &&
      matchSelector(selector, (p as { metadata?: { labels?: Record<string, string> } }).metadata?.labels),
  );
}

/** Events about the object: `regarding` (events.k8s.io) or `involvedObject` (core), newest first. */
export function eventsFor(uid: string, events: K8sObject[]): K8sObject[] {
  const time = (e: K8sObject) =>
    Date.parse(
      getString(e, 'series.lastObservedTime') ||
        getString(e, 'eventTime') ||
        getString(e, 'deprecatedLastTimestamp') ||
        getString(e, 'lastTimestamp') ||
        getString(e, 'metadata.creationTimestamp'),
    ) || 0;
  return events
    .filter(
      (e) =>
        uid !== '' && (getString(e, 'regarding.uid') === uid || getString(e, 'involvedObject.uid') === uid),
    )
    .sort((a, b) => time(b) - time(a));
}

export { uidOf };
