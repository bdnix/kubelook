import { Link } from 'react-router';
import { clusterPath, resourcePath } from '../../lib/paths';
import type { FailingPod } from './triage';

/** Pods that failed or whose containers fail to start. */
export function FailingPods({ cluster, pods }: { cluster: string; pods: FailingPod[] }) {
  return (
    <section aria-label="Failing pods">
      <h2>Failing pods</h2>
      <p>
        <Link to={`${clusterPath(cluster)}/core/Pod`}>{`${pods.length} failing`}</Link>
      </p>
      <ul>
        {pods.map((p) => (
          <li key={`${p.namespace}/${p.name}`}>
            <Link
              to={resourcePath({ cluster, namespace: p.namespace, group: 'core', kind: 'Pod', name: p.name })}
            >
              {p.name}
            </Link>{' '}
            {p.reason}
          </li>
        ))}
      </ul>
    </section>
  );
}
