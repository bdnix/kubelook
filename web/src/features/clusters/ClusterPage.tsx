import { Navigate, Route, Routes, useParams } from 'react-router';
import { clusterPath } from '../../lib/paths';
import { ResourceListPage } from '../resources/ResourceListPage';

function ListRoute({ cluster }: { cluster: string }) {
  const { group = 'core', kind = 'Pod' } = useParams();
  return <ResourceListPage cluster={cluster} group={group} kind={kind} />;
}

/** The pages of one cluster: the Pods list by default, any other kind by `/c/<cluster>/<group>/<kind>`. */
export function ClusterPage() {
  const { cluster = '' } = useParams();
  return (
    <Routes>
      {/* Absolute: a relative link would encode the already encoded ID a second time. */}
      <Route index element={<Navigate to={`${clusterPath(cluster)}/core/Pod`} replace />} />
      <Route path=":group/:kind" element={<ListRoute cluster={cluster} />} />
      {/* The details drawer is its own route in the layout; the list stays behind it. */}
      <Route path=":group/:kind/:name" element={<ListRoute cluster={cluster} />} />
      <Route path="ns/:namespace/:group/:kind/:name" element={<ListRoute cluster={cluster} />} />
      <Route path="*" element={<p>Page not found</p>} />
    </Routes>
  );
}
