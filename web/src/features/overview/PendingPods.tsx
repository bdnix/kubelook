import { Link } from 'react-router';
import { clusterPath, resourcePath } from '../../lib/paths';
import type { PendingPod } from './triage';

/** Pending pods with the message of their latest FailedScheduling event. */
export function PendingPods({ cluster, pods }: { cluster: string; pods: PendingPod[] }) {
  return (
    <section aria-label="Pending pods">
      <h2>Pending pods</h2>
      <p>
        <Link
          to={`${clusterPath(cluster)}/core/Pod?filter=${encodeURIComponent('status.phase = Pending')}`}
        >{`${pods.length} pending`}</Link>
      </p>
      <ul>
        {pods.map((p) => (
          <li key={`${p.namespace}/${p.name}`}>
            <Link
              to={resourcePath({ cluster, namespace: p.namespace, group: 'core', kind: 'Pod', name: p.name })}
            >
              {p.name}
            </Link>{' '}
            {p.message}
          </li>
        ))}
      </ul>
    </section>
  );
}
