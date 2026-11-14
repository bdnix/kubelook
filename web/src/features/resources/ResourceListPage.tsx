import { findResource, useDiscovery } from '../../api/discovery';
import type { K8sObject } from '../../state/live';
import { useOpenCluster } from '../../api/open';
import { Banner } from '../../components/Banner';
import { Table } from '../../components/Table';
import { getKind } from '../../kinds/registry';
import { useNow } from '../../lib/clock';
import { getArray, getString } from '../../lib/path';
import { logTab, useDock } from '../../state/dock';
import { useLive } from '../../state/useLive';

const MINUTE = 60_000;

/** The live list of one kind of an open cluster. */
export function ResourceListPage({ cluster, group, kind }: { cluster: string; group: string; kind: string }) {
  const opened = useOpenCluster(cluster);
  const discovery = useDiscovery(cluster, opened.status === 'open');
  const resource = discovery.data ? findResource(discovery.data, group, kind) : undefined;
  const live = useLive(
    resource
      ? { cluster, group, version: resource.version, resource: resource.resource, namespace: '' }
      : undefined,
  );
  const now = useNow(MINUTE);
  const descriptor = getKind(group, kind);
  const openDock = useDock().open;

  const openLogs = (pod: K8sObject) => {
    const containers = getArray(pod, 'spec.containers')
      .map((c) => getString(c, 'name'))
      .filter((n) => n !== '');
    openDock(
      logTab({
        cluster,
        namespace: getString(pod, 'metadata.namespace'),
        pod: getString(pod, 'metadata.name'),
        containers: containers.length > 0 ? containers : [''],
      }),
    );
  };

  if (opened.status === 'error') return <Banner kind="error">{opened.error.message}</Banner>;
  if (opened.status === 'opening') return <p role="status">Opening the cluster…</p>;
  if (discovery.isError) return <Banner kind="error">{discovery.error.message}</Banner>;
  if (!discovery.data) return <p role="status">Loading…</p>;
  if (!resource) return <p>{`This cluster does not serve ${kind}.`}</p>;

  if (live.status === 'error') {
    if (live.error?.code === 'forbidden')
      return <p>{`You don't have permission to list ${descriptor.title}.`}</p>;
    return <Banner kind="error">{live.error?.message ?? 'Something went wrong.'}</Banner>;
  }

  return (
    <>
      <h1>{descriptor.title}</h1>
      {live.status === 'paused' && (
        <Banner kind="info">Live updates paused. They resume when this tab is visible.</Banner>
      )}
      {live.status === 'loading' ? (
        <p role="status">Loading…</p>
      ) : (
        <Table
          label={descriptor.title}
          columns={descriptor.columns}
          rows={live.items}
          getKey={(o) =>
            getString(o, 'metadata.uid') ||
            `${getString(o, 'metadata.namespace')}/${getString(o, 'metadata.name')}`
          }
          now={now}
          statusOf={descriptor.status}
          onOpen={descriptor.kind === 'Pod' && descriptor.group === '' ? (pod) => openLogs(pod) : undefined}
        />
      )}
    </>
  );
}
