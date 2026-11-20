import { Navigate, Route, Routes, useParams } from 'react-router';
import { useClusters } from './useClusters';
import { NoAccessPage } from '../shell/NoAccessPage';
import { clusterPath } from '../../lib/paths';
import { PodFilesPage } from '../files/PodFilesPage';
import { WhoCanPage } from '../rbac/WhoCanPage';
import { ReleasesPage } from '../helm/ReleasesPage';
import { ModulePage } from '../modules/ModulePage';
import { ResourceListPage } from '../resources/ResourceListPage';

function ListRoute({ cluster }: { cluster: string }) {
  const { group = 'core', kind = 'Pod' } = useParams();
  return <ResourceListPage cluster={cluster} group={group} kind={kind} />;
}

/** The pages of one cluster: the Pods list by default, any other kind by `/c/<cluster>/<group>/<kind>`. */
export function ClusterPage() {
  const { cluster = '' } = useParams();
  const clusters = useClusters();
  // A link to a cluster that is not in the session (a kubeconfig that was never uploaded, or one that is gone).
  if (clusters.data && !clusters.data.some((s) => s.clusters.some((c) => c.id === cluster))) {
    return <NoAccessPage reason="unknown" />;
  }
  return (
    <Routes>
      {/* Absolute: a relative link would encode the already encoded ID a second time. */}
      <Route index element={<Navigate to={`${clusterPath(cluster)}/core/Pod`} replace />} />
      <Route path="pod-files" element={<PodFilesPage cluster={cluster} />} />
      <Route path="modules/:name" element={<ModulePage cluster={cluster} />} />
      <Route path="helm" element={<ReleasesPage cluster={cluster} />} />
      <Route path="who-can" element={<WhoCanPage cluster={cluster} />} />
      <Route path=":group/:kind" element={<ListRoute cluster={cluster} />} />
      {/* The details drawer is its own route in the layout; the list stays behind it. */}
      <Route path=":group/:kind/:name" element={<ListRoute cluster={cluster} />} />
      <Route path="ns/:namespace/:group/:kind/:name" element={<ListRoute cluster={cluster} />} />
      <Route path="*" element={<p>Page not found</p>} />
    </Routes>
  );
}
