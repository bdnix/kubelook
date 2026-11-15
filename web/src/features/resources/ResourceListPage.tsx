import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router';
import { findResource, useDiscovery } from '../../api/discovery';
import type { K8sObject } from '../../state/live';
import { useOpenCluster } from '../../api/open';
import { useSettings } from '../../api/settings';
import { Banner } from '../../components/Banner';
import { Button } from '../../components/Button';
import { Table } from '../../components/Table';
import { BulkBar } from '../actions/BulkBar';
import { ExportMenu } from '../export/ExportMenu';
import { applyPreset, presetKey } from '../settings/columnPreset';
import { DownloadMenu } from '../filter/DownloadMenu';
import { FilterBar } from '../filter/FilterBar';
import { SavedFilters } from '../filter/SavedFilters';
import { useFilter } from '../filter/useFilter';
import { CreateFromYaml } from '../editor/CreateFromYaml';
import { getKind } from '../../kinds/registry';
import { useNow } from '../../lib/clock';
import { getString } from '../../lib/path';
import { resourcePath } from '../../lib/paths';
import { keepNamespaces, subscriptionPlan } from '../../lib/namespaces';
import { useLiveMany } from '../../state/useLiveMany';
import { metricsColumns } from '../metrics/columns';
import { useNodeMetrics, usePodMetrics } from '../metrics/useMetrics';
import { NamespaceFilter } from '../namespaces/NamespaceFilter';
import { NoAccessPage } from '../shell/NoAccessPage';
import { ApiError } from '../../api/client';

const MINUTE = 60_000;

/** The live list of one kind of an open cluster. */
export function ResourceListPage({ cluster, group, kind }: { cluster: string; group: string; kind: string }) {
  const opened = useOpenCluster(cluster);
  const discovery = useDiscovery(cluster, opened.status === 'open');
  const resource = discovery.data ? findResource(discovery.data, group, kind) : undefined;
  const { settings, update, loaded } = useSettings();
  const savedNamespaces = settings?.namespaceFilters[cluster];
  const plan = useMemo(() => subscriptionPlan(savedNamespaces ?? []), [savedNamespaces]);
  const live = useLiveMany(
    resource && loaded
      ? plan.namespaces.map((namespace) => ({
          cluster,
          group,
          version: resource.version,
          resource: resource.resource,
          namespace: resource.namespaced ? namespace : '',
        }))
      : undefined,
  );
  const namespaced = resource?.namespaced ?? false;
  // kept as it is while nothing changed: a new array on every render would make the table report its rows again and again
  const items = useMemo(
    () => keepNamespaces(live.items, namespaced ? plan.clientFilter : undefined),
    [live.items, namespaced, plan],
  );
  const now = useNow(MINUTE);
  const descriptor = getKind(group, kind);
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const isPods = descriptor.group === '' && descriptor.kind === 'Pod';
  const isNodes = descriptor.group === '' && descriptor.kind === 'Node';
  const podUsage = usePodMetrics(cluster, '', isPods && opened.status === 'open');
  const nodeUsage = useNodeMetrics(cluster, isNodes && opened.status === 'open');
  const usage = isPods ? podUsage : nodeUsage;
  // CPU and memory columns appear when the cluster serves metrics
  const withUsage =
    (isPods || isNodes) && usage.data?.available
      ? [...descriptor.columns, ...metricsColumns(usage.byKey, isPods ? 'pod' : 'node')]
      : descriptor.columns;
  const columns = applyPreset(
    withUsage,
    settings?.columnPresets[presetKey(descriptor.group, descriptor.kind)],
  );
  const [query, setQuery] = useState('');
  const [shown, setShown] = useState<K8sObject[]>([]);
  const filtered = useFilter(items, query, { group: descriptor.group, kind: descriptor.kind }, now);

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

  if (opened.status === 'error') {
    if (opened.error instanceof ApiError && opened.error.code === 'forbidden')
      return <NoAccessPage reason="forbidden" />;
    return <Banner kind="error">{opened.error.message}</Banner>;
  }
  if (opened.status === 'opening') return <p role="status">Opening the cluster…</p>;
  if (discovery.isError) return <Banner kind="error">{discovery.error.message}</Banner>;
  if (!discovery.data) return <p role="status">Loading…</p>;
  if (!resource) return <p>{`This cluster does not serve ${kind}.`}</p>;

  if (live.status === 'error') {
    if (live.error?.code === 'forbidden')
      return <p>{`You don't have permission to list ${descriptor.title}.`}</p>;
    return <Banner kind="error">{live.error?.message ?? 'Something went wrong.'}</Banner>;
  }

  const keyOf = (o: K8sObject) =>
    getString(o, 'metadata.uid') || `${getString(o, 'metadata.namespace')}/${getString(o, 'metadata.name')}`;
  const chosen = new Set(selected);
  const selectedObjects = filtered.rows.filter((o) => chosen.has(keyOf(o)));

  return (
    <>
      <h1>{descriptor.title}</h1>
      {descriptor.note && <p>{descriptor.note}</p>}
      <Button onClick={() => setCreating(!creating)}>{creating ? 'Close' : 'Create from YAML'}</Button>
      {creating && <CreateFromYaml cluster={cluster} />}
      {live.status === 'paused' && (
        <Banner kind="info">Live updates paused. They resume when this tab is visible.</Banner>
      )}
      {resource.namespaced && <NamespaceFilter cluster={cluster} />}
      <FilterBar
        kind={{ group: descriptor.group, kind: descriptor.kind }}
        objects={items}
        value={query}
        onChange={setQuery}
      />
      <SavedFilters
        kind={descriptor.kind}
        sets={settings?.filterSets ?? []}
        query={query}
        onLoad={setQuery}
        onSave={(set) => void update((s) => ({ ...s, filterSets: [...s.filterSets, set] }))}
        onRename={(from, to) =>
          void update((s) => ({
            ...s,
            filterSets: s.filterSets.map((f) =>
              f.kind === descriptor.kind && f.name === from ? { ...f, name: to } : f,
            ),
          }))
        }
        onDelete={(name) =>
          void update((s) => ({
            ...s,
            filterSets: s.filterSets.filter((f) => !(f.kind === descriptor.kind && f.name === name)),
          }))
        }
      />
      <ExportMenu cluster={cluster} kind={resource.resource} columns={columns} rows={shown} now={now} />
      <DownloadMenu
        cluster={cluster}
        kind={descriptor.kind}
        resource={descriptor.group === '' ? resource.resource : `${resource.resource}.${descriptor.group}`}
        namespace={descriptor.namespaced ? '' : null}
        objects={filtered.rows}
        filter={filtered.filter}
      />
      {live.status === 'loading' ? (
        <p role="status">Loading…</p>
      ) : (
        <Table
          label={descriptor.title}
          columns={columns}
          rows={filtered.rows}
          getKey={keyOf}
          now={now}
          statusOf={descriptor.status}
          onOpen={open}
          onSelectionChange={setSelected}
          onShownChange={setShown}
        />
      )}
      <BulkBar
        cluster={cluster}
        group={group === 'core' ? '' : group}
        version={resource.version}
        resource={resource.resource}
        kind={descriptor.kind}
        objects={selectedObjects}
      />
    </>
  );
}
