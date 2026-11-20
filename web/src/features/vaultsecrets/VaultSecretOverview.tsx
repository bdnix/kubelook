import { getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { useKindItems } from '../details/useDetails';
import { findConsumers } from './consumers';
import { VaultSecretDetails } from './VaultSecretDetails';

/** The Overview section of a VaultSecret: it reads the workloads of the namespace to find the consumers. */
export function VaultSecretOverview({
  cluster,
  object,
  now,
}: {
  cluster: string;
  object: K8sObject;
  now: number;
}) {
  const namespace = getString(object, 'metadata.namespace');
  const secret = getString(object, 'spec.name') || getString(object, 'metadata.name');
  const stamp = (kind: string, items: K8sObject[]) => items.map((o) => ({ kind, ...o }));
  const deployments = useKindItems(cluster, 'apps', 'Deployment', namespace, true);
  const statefulSets = useKindItems(cluster, 'apps', 'StatefulSet', namespace, true);
  const daemonSets = useKindItems(cluster, 'apps', 'DaemonSet', namespace, true);
  const consumers = findConsumers(secret, namespace, [
    ...stamp('Deployment', deployments),
    ...stamp('StatefulSet', statefulSets),
    ...stamp('DaemonSet', daemonSets),
  ]);
  return <VaultSecretDetails cluster={cluster} object={object} consumers={consumers} now={now} />;
}
