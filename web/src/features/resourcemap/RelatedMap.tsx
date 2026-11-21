import { useNavigate } from 'react-router';
import { Banner } from '../../components/Banner';
import { Graph } from '../../components/Graph';
import { Legend, mapNodes } from './legend';
import { openNode } from './ResourceMapPage';
import { useResourceMap } from './useResourceMap';

/** The objects linked to one object, for its details. */
export function RelatedMap({
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
  const navigate = useNavigate();
  const map = useResourceMap({
    cluster,
    namespaces: [namespace],
    focus: { group: group === 'core' ? '' : group, kind, namespace, name },
  });
  const nodes = map.graph?.nodes ?? [];
  return (
    <section aria-label="Related resources">
      {map.status === 'error' && <Banner kind="error">{map.error}</Banner>}
      {map.status === 'loading' && <p role="status">Loading…</p>}
      {map.graph && (
        <Graph
          label="Related resources"
          nodes={mapNodes(nodes)}
          edges={map.graph.edges}
          onOpen={(g) => {
            const n = nodes.find((x) => x.id === g.id);
            if (n) void navigate(openNode(cluster, n));
          }}
        />
      )}
      <Legend />
    </section>
  );
}
