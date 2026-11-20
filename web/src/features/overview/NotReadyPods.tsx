import { Link } from 'react-router';
import { resourcePath } from '../../lib/paths';
import type { NotReadyPod } from './triage';

/** Running pods that are not ready, with the failing condition and the containers that are not ready. */
export function NotReadyPods({ cluster, pods }: { cluster: string; pods: NotReadyPod[] }) {
  return (
    <section aria-label="Pods that are not ready">
      <h2>Pods that are not ready</h2>
      <p>{`${pods.length} not ready`}</p>
      <ul>
        {pods.map((p) => (
          <li key={`${p.namespace}/${p.name}`}>
            <Link
              to={resourcePath({ cluster, namespace: p.namespace, group: 'core', kind: 'Pod', name: p.name })}
            >
              {p.name}
            </Link>{' '}
            {p.message}
            {p.containers.length > 0 && ` (not ready: ${p.containers.join(', ')})`}
          </li>
        ))}
      </ul>
    </section>
  );
}
