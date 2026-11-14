import { useCallback } from 'react';
import { useNavigate, useParams } from 'react-router';
import { clusterPath } from '../../lib/paths';
import { DetailsDrawer } from './DetailsDrawer';

/** The drawer for the deep link `/c/<cluster>/[ns/<ns>/]<group>/<kind>/<name>`; closing returns to the list. */
export function DrawerRoute() {
  const { cluster = '', namespace = '', group = 'core', kind = '', name = '' } = useParams();
  const navigate = useNavigate();
  const close = useCallback(
    () => void navigate(`${clusterPath(cluster)}/${encodeURIComponent(group)}/${encodeURIComponent(kind)}`),
    [navigate, cluster, group, kind],
  );
  return (
    <DetailsDrawer
      key={`${cluster}/${namespace}/${group}/${kind}/${name}`}
      cluster={cluster}
      group={group === 'core' ? '' : group}
      kind={kind}
      namespace={namespace}
      name={name}
      onClose={close}
    />
  );
}
