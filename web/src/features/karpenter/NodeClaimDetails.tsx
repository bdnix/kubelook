import { Link } from 'react-router';
import { getArray, getString } from '../../lib/path';
import { resourcePath } from '../../lib/paths';
import type { K8sObject } from '../../state/live';

const labelOf = (o: K8sObject, key: string) =>
  ((o.metadata as { labels?: Record<string, string> } | undefined)?.labels ?? {})[key] ?? '';

/** The Karpenter section of a NodeClaim: lifecycle, facts, links, and why it is not ready. */
export function NodeClaimDetails({ cluster, object }: { cluster: string; object: K8sObject }) {
  const conditions = getArray(object, 'status.conditions');
  const falseOne = conditions.find((c) => getString(c, 'status') === 'False');
  const node = getString(object, 'status.nodeName');
  const pool = labelOf(object, 'karpenter.sh/nodepool');
  const classRef = (
    object.spec as { nodeClassRef?: { group?: string; kind?: string; name?: string } } | undefined
  )?.nodeClassRef;
  const link = (group: string, kind: string, name: string) => (
    <Link to={resourcePath({ cluster, group: group || 'core', kind, name })}>{name}</Link>
  );
  return (
    <section aria-label="Karpenter">
      <h3>Karpenter</h3>
      {falseOne !== undefined && (
        <>
          <h4>Why it is not ready</h4>
          <p>
            {getString(falseOne, 'message') || getString(falseOne, 'reason') || getString(falseOne, 'type')}
          </p>
        </>
      )}
      <ul aria-label="Lifecycle">
        {['Launched', 'Registered', 'Initialized', 'Drifted'].map((t) => {
          const c = conditions.find((x) => getString(x, 'type') === t);
          return c ? (
            <li
              key={t}
            >{`${t}: ${getString(c, 'status')}${getString(c, 'reason') ? ` (${getString(c, 'reason')})` : ''}`}</li>
          ) : null;
        })}
      </ul>
      <dl>
        <dt>Instance type</dt>
        <dd>{labelOf(object, 'node.kubernetes.io/instance-type')}</dd>
        <dt>Capacity type</dt>
        <dd>{labelOf(object, 'karpenter.sh/capacity-type')}</dd>
        <dt>Zone</dt>
        <dd>{labelOf(object, 'topology.kubernetes.io/zone')}</dd>
        <dt>Provider ID</dt>
        <dd>{getString(object, 'status.providerID')}</dd>
        <dt>Image ID</dt>
        <dd>{getString(object, 'status.imageID')}</dd>
        <dt>Capacity</dt>
        <dd>
          {Object.entries(
            (object.status as { capacity?: Record<string, string> } | undefined)?.capacity ?? {},
          )
            .map(([k, v]) => `${k}: ${v}`)
            .join(', ')}
        </dd>
        <dt>Node</dt>
        <dd>{node ? link('', 'Node', node) : 'not registered'}</dd>
        <dt>NodePool</dt>
        <dd>{pool ? link('karpenter.sh', 'NodePool', pool) : ''}</dd>
        <dt>Node class</dt>
        <dd>
          {classRef?.name && classRef.kind ? link(classRef.group ?? '', classRef.kind, classRef.name) : ''}
        </dd>
      </dl>
    </section>
  );
}
