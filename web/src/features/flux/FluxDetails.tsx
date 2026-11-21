import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import { ApiError } from '../../api/client';
import { useApi } from '../../api/context';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { findFluxKind, isSuspended } from '../../kinds/flux';
import { getArray, getString } from '../../lib/path';
import { resourcePath } from '../../lib/paths';
import type { K8sObject } from '../../state/live';
import { useAction } from '../actions/useAction';
import { Conditions } from '../details/Conditions';
import { inventoryUrl } from './FluxGraph';
import type { InventoryEntry } from './graph';

const WITH_INVENTORY = ['Kustomization', 'HelmRelease'];
const RESUME_FIRST = 'Resume the object before reconciling.';

function Row({ label, value }: { label: string; value: string }) {
  if (value === '') return null;
  return (
    <>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </>
  );
}

function Inventory({
  cluster,
  group,
  kind,
  namespace,
  name,
}: {
  cluster: string;
  group: string;
  kind: string;
  namespace: string;
  name: string;
}) {
  const api = useApi();
  const query = useQuery({
    queryKey: ['flux-inventory', cluster, group, kind, namespace, name],
    queryFn: () =>
      api.get<{ entries: InventoryEntry[]; note?: string }>(
        inventoryUrl(cluster, group, kind, namespace, name),
      ),
    retry: false,
  });
  if (query.isError)
    return (
      <ErrorText
        message={query.error instanceof ApiError ? query.error.message : 'The inventory could not be read.'}
      />
    );
  if (!query.data) return <p role="status">Loading the inventory…</p>;
  return (
    <section aria-label="Inventory">
      <h3>Inventory</h3>
      {query.data.note && <p>{query.data.note}</p>}
      <ul aria-label="Inventory">
        {query.data.entries.map((e) => (
          <li key={`${e.group}/${e.kind}/${e.namespace}/${e.name}`}>
            <Link
              to={resourcePath({
                cluster,
                namespace: e.namespace,
                group: e.group,
                kind: e.kind,
                name: e.name,
              })}
            >{`${e.kind} ${e.name}`}</Link>
          </li>
        ))}
      </ul>
    </section>
  );
}

export interface FluxDetailsProps {
  cluster: string;
  group: string;
  version: string;
  resource: string;
  kind: string;
  object: K8sObject;
  now: number;
}

/** The Overview of a Flux object: conditions, revisions, inventory, history and the reconcile, suspend and resume buttons. */
export function FluxDetails({ cluster, group, version, resource, kind, object, now }: FluxDetailsProps) {
  const def = findFluxKind(group, kind);
  const { run, busy, error } = useAction(cluster);
  if (!def) return null;
  const namespace = getString(object, 'metadata.namespace');
  const name = getString(object, 'metadata.name');
  const suspended = isSuspended(def, object);
  const target = { group, version, resource, namespace, name };
  const history = getArray(object, 'status.history');
  return (
    <section aria-label="Flux">
      <h3>Flux</h3>
      <div>
        <Button
          disabled={suspended || busy}
          title={suspended ? RESUME_FIRST : undefined}
          onClick={() => void run('fluxReconcile', target)}
        >
          Reconcile
        </Button>
        {def.via !== 'never' && (
          <>
            <Button disabled={suspended || busy} onClick={() => void run('fluxSuspend', target)}>
              Suspend
            </Button>
            <Button disabled={!suspended || busy} onClick={() => void run('fluxResume', target)}>
              Resume
            </Button>
          </>
        )}
      </div>
      <ErrorText message={error} />
      <dl>
        <Row label="Last applied revision" value={getString(object, 'status.lastAppliedRevision')} />
        <Row label="Last attempted revision" value={getString(object, 'status.lastAttemptedRevision')} />
        <Row label="Artifact revision" value={getString(object, 'status.artifact.revision')} />
      </dl>
      <Conditions conditions={getArray(object, 'status.conditions')} now={now} />
      {WITH_INVENTORY.includes(kind) && (
        <Inventory cluster={cluster} group={group} kind={kind} namespace={namespace} name={name} />
      )}
      {kind === 'HelmRelease' && history.length > 0 && (
        <table aria-label="History">
          <thead>
            <tr>
              <th>Chart</th>
              <th>Chart version</th>
              <th>Status</th>
              <th>Release</th>
            </tr>
          </thead>
          <tbody>
            {history.map((h, i) => (
              <tr key={i}>
                <td>{getString(h, 'chartName')}</td>
                <td>{getString(h, 'chartVersion')}</td>
                <td>{getString(h, 'status')}</td>
                <td>{`${getString(h, 'namespace')}/${getString(h, 'name')}`}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
