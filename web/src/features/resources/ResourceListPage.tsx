import { useState } from 'react';
import { useNavigate } from 'react-router';
import { findResource, useDiscovery } from '../../api/discovery';
import type { K8sObject } from '../../state/live';
import { useOpenCluster } from '../../api/open';
import { Banner } from '../../components/Banner';
import { Button } from '../../components/Button';
import { Table } from '../../components/Table';
import { CreateFromYaml } from '../editor/CreateFromYaml';
import { getKind } from '../../kinds/registry';
import { useNow } from '../../lib/clock';
import { getString } from '../../lib/path';
import { resourcePath } from '../../lib/paths';
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
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);

  const open = (o: K8sObject) =>
    void navigate(
      resourcePath({
        cluster,
        namespace: getString(o, 'metadata.namespace'),
        group,
        kind,
        name: getString(o, 'metadata.name'),
      }),
    );

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
      {descriptor.note && <p>{descriptor.note}</p>}
      <Button onClick={() => setCreating(!creating)}>{creating ? 'Close' : 'Create from YAML'}</Button>
      {creating && <CreateFromYaml cluster={cluster} />}
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
          onOpen={open}
        />
      )}
    </>
  );
}
