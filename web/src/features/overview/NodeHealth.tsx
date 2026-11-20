import { Link } from 'react-router';
import { clusterPath } from '../../lib/paths';
import type { NodeCounts } from './triage';

/** How many nodes are ready, not ready, cordoned or under pressure. */
export function NodeHealth({ cluster, counts }: { cluster: string; counts: NodeCounts }) {
  return (
    <section aria-label="Nodes">
      <h2>Nodes</h2>
      <p>
        <Link to={`${clusterPath(cluster)}/core/Node`}>{`${counts.total} nodes`}</Link>
      </p>
      <ul>
        <li>
          Ready <strong>{counts.ready}</strong>
        </li>
        <li>
          Not ready <strong>{counts.notReady}</strong>
        </li>
        <li>
          Cordoned <strong>{counts.cordoned}</strong>
        </li>
        <li>
          Under pressure <strong>{counts.underPressure}</strong>
        </li>
      </ul>
    </section>
  );
}
