import { useNavigate } from 'react-router';
import { useSettings } from '../../api/settings';
import { Banner } from '../../components/Banner';
import { Graph } from '../../components/Graph';
import { resourcePath } from '../../lib/paths';
import { NamespaceFilter } from '../namespaces/NamespaceFilter';
import { Legend, mapNodes, type MapNode } from './legend';
import { useResourceMap } from './useResourceMap';

/** Opens the details of a node. The core group is written `core` in paths. */
export function openNode(cluster: string, n: MapNode) {
  return resourcePath({
    cluster,
    namespace: n.namespace,
    group: n.group || 'core',
    kind: n.kind,
    name: n.name,
  });
}

/** The live map of the objects of the selected namespaces. */
export function ResourceMapPage({ cluster }: { cluster: string }) {
  const { settings, loaded } = useSettings();
  const navigate = useNavigate();
  const namespaces = settings?.namespaceFilters[cluster] ?? [];
  const map = useResourceMap(loaded ? { cluster, namespaces } : undefined);
  const nodes = map.graph?.nodes ?? [];
  return (
    <>
      <h1>Resource Map</h1>
      <NamespaceFilter cluster={cluster} />
      {map.status === 'error' && <Banner kind="error">{map.error}</Banner>}
      {map.graph && map.graph.omittedKinds.length > 0 && (
        <Banner kind="info">{`You may not list ${map.graph.omittedKinds.join(', ')}, so the map is drawn without them.`}</Banner>
      )}
      {map.graph?.leftOutNamespaces && map.graph.leftOutNamespaces.length > 0 && (
        <Banner kind="info">{`${map.graph.leftOutNamespaces.join(', ')} could not be shown: a map watches at most 24 lists. Select fewer namespaces.`}</Banner>
      )}
      {map.status === 'loading' && <p role="status">Loading…</p>}
      {map.graph && (
        <Graph
          label="Resource map"
          nodes={mapNodes(nodes)}
          edges={map.graph.edges}
          onOpen={(g) => {
            const n = nodes.find((x) => x.id === g.id);
            if (n) void navigate(openNode(cluster, n));
          }}
        />
      )}
      <Legend />
    </>
  );
}
