import { Link } from 'react-router';
import { formatAge } from '../../lib/age';
import { getArray, getNumber, getString } from '../../lib/path';
import { resourcePath } from '../../lib/paths';
import type { K8sObject } from '../../state/live';
import { ConsumersPanel } from './ConsumersPanel';
import type { Consumer } from './consumers';

/** The Overview of a VaultSecret: its SecretCreated condition, the Secret it makes and the workloads that use it. */
export function VaultSecretDetails({
  cluster,
  object,
  consumers,
  now,
}: {
  cluster: string;
  object: K8sObject;
  consumers: Consumer[];
  now: number;
}) {
  const namespace = getString(object, 'metadata.namespace');
  const secret = getString(object, 'spec.name') || getString(object, 'metadata.name');
  const condition = getArray(object, 'status.conditions').find(
    (c) => getString(c, 'type') === 'SecretCreated',
  );
  const stale =
    condition !== undefined &&
    getNumber(condition, 'observedGeneration') < getNumber(object, 'metadata.generation');
  return (
    <section aria-label="Vault Secrets Operator">
      <h3>Vault Secrets Operator</h3>
      {condition !== undefined && (
        <dl>
          <dt>SecretCreated</dt>
          <dd>{getString(condition, 'status')}</dd>
          <dt>Reason</dt>
          <dd>{getString(condition, 'reason')}</dd>
          <dt>Message</dt>
          <dd>{getString(condition, 'message')}</dd>
          <dt>Last transition</dt>
          <dd>{formatAge(getString(condition, 'lastTransitionTime'), now)}</dd>
        </dl>
      )}
      {stale && <span>stale</span>}
      <p>
        <Link
          to={resourcePath({ cluster, namespace, group: 'core', kind: 'Secret', name: secret })}
        >{`Secret ${secret}`}</Link>
      </p>
      <ConsumersPanel cluster={cluster} consumers={consumers} />
    </section>
  );
}
