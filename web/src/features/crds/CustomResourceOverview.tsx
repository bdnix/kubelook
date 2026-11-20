import { findResource, useDiscovery } from '../../api/discovery';
import { crdSchema } from '../../kinds/generic';
import type { K8sObject } from '../../state/live';
import { SchemaDetails } from './SchemaDetails';
import { useCrd } from './useCrd';

/** The Overview of a custom resource: its spec and status, with descriptions from the CRD when it can be read. */
export function CustomResourceOverview({
  cluster,
  group,
  kind,
  object,
}: {
  cluster: string;
  group: string;
  kind: string;
  object: K8sObject;
}) {
  const discovery = useDiscovery(cluster);
  const resource = discovery.data ? findResource(discovery.data, group, kind) : undefined;
  const crd = useCrd(cluster, group, resource?.resource ?? '', resource !== undefined);
  return <SchemaDetails schema={crdSchema(crd, resource?.version ?? '')} object={object} />;
}
