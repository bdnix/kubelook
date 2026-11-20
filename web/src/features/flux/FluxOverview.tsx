import { findResource, useDiscovery } from '../../api/discovery';
import type { K8sObject } from '../../state/live';
import { FluxDetails } from './FluxDetails';

/** The Flux section of the Overview of a Flux object; it needs the version the cluster serves. */
export function FluxOverview({
  cluster,
  group,
  kind,
  object,
  now,
}: {
  cluster: string;
  group: string;
  kind: string;
  object: K8sObject;
  now: number;
}) {
  const discovery = useDiscovery(cluster);
  const resource = discovery.data ? findResource(discovery.data, group, kind) : undefined;
  if (!resource) return null;
  return (
    <FluxDetails
      cluster={cluster}
      group={group}
      version={resource.version}
      resource={resource.resource}
      kind={kind}
      object={object}
      now={now}
    />
  );
}
