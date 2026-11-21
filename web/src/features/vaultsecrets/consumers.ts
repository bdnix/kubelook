import { getArray, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';

export interface Consumer {
  group: string;
  version: string;
  resource: string;
  kind: string;
  namespace: string;
  name: string;
}

const PLURAL: Record<string, string> = {
  Deployment: 'deployments',
  StatefulSet: 'statefulsets',
  DaemonSet: 'daemonsets',
};

function usesSecret(workload: K8sObject, secret: string): boolean {
  const podSpec = 'spec.template.spec';
  const containers = [
    ...getArray(workload, `${podSpec}.containers`),
    ...getArray(workload, `${podSpec}.initContainers`),
  ];
  for (const c of containers) {
    if (getArray(c, 'envFrom').some((e) => getString(e, 'secretRef.name') === secret)) return true;
    if (getArray(c, 'env').some((e) => getString(e, 'valueFrom.secretKeyRef.name') === secret)) return true;
  }
  return getArray(workload, `${podSpec}.volumes`).some((v) => getString(v, 'secret.secretName') === secret);
}

/** The Deployments, StatefulSets and DaemonSets of the namespace whose pods use the Secret. */
export function findConsumers(secret: string, namespace: string, workloads: K8sObject[]): Consumer[] {
  return workloads
    .filter((w) => getString(w, 'metadata.namespace') === namespace && usesSecret(w, secret))
    .map((w) => ({
      group: 'apps',
      version: 'v1',
      resource: PLURAL[getString(w, 'kind')] ?? '',
      kind: getString(w, 'kind'),
      namespace,
      name: getString(w, 'metadata.name'),
    }));
}
