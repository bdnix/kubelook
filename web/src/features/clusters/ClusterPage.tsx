import { Navigate, Route, Routes, useParams } from 'react-router';
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
      <Route index element={<Navigate to="core/Pod" replace />} />
      <Route path=":group/:kind" element={<ListRoute cluster={cluster} />} />
      <Route path="*" element={<p>Page not found</p>} />
    </Routes>
  );
}
