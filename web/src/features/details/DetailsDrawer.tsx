import { useEffect, useState } from 'react';
import { useAccess } from '../../api/access';
import { Banner } from '../../components/Banner';
import { Button } from '../../components/Button';
import { StatusBadge } from '../../components/StatusBadge';
import { getKind } from '../../kinds/registry';
import { useNow } from '../../lib/clock';
import { getArray, getString } from '../../lib/path';
import { childrenOf, eventsFor, podsOfWorkload } from '../../lib/related';
import { logTab, useDock } from '../../state/dock';
import type { K8sObject } from '../../state/live';
import { ActionMenu } from '../actions/ActionMenu';
import type { Revision } from '../actions/RollbackDialog';
import { EditorTab } from '../editor/EditorTab';
import { DebugButton } from '../debug/DebugButton';
import { NodeShellButton } from '../debug/NodeShellButton';
import { OpenShell } from '../shell/OpenShell';
import { EventsTab } from './EventsTab';
import { OverviewTab } from './OverviewTab';
import { RelatedTab } from './RelatedTab';
import { useFocusReturn, useKindItems, useObject, useOwnerChain, useResourceInfo } from './useDetails';

export type TabId = 'overview' | 'yaml' | 'events' | 'logs' | 'files' | 'related';

const MINUTE = 60_000;
const WORKLOADS = ['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet', 'Job'];
const NEEDS_PODS = ['Service', 'PersistentVolumeClaim', ...WORKLOADS];

export interface DrawerProps {
  cluster: string;
  group: string;
  kind: string;
  /** Empty for cluster-scoped kinds. */
  namespace: string;
  name: string;
  onClose: () => void;
}

function tabsFor(group: string, kind: string): { id: TabId; title: string }[] {
  const core = group === '' || group === 'core';
  const tabs: { id: TabId; title: string }[] = [
    { id: 'overview', title: 'Overview' },
    { id: 'yaml', title: 'YAML' },
    { id: 'events', title: 'Events' },
  ];
  if ((core && kind === 'Pod') || WORKLOADS.includes(kind)) tabs.push({ id: 'logs', title: 'Logs' });
  if (core && kind === 'Pod') tabs.push({ id: 'files', title: 'Files' });
  tabs.push({ id: 'related', title: 'Related' });
  return tabs;
}

/** The earlier revisions of a Deployment: its ReplicaSets with a revision annotation, except the current one. */
function revisionsOf(deployment: K8sObject, replicaSets: K8sObject[]): Revision[] {
  const key = 'deployment.kubernetes.io/revision';
  const annotations = (o: K8sObject) =>
    (o.metadata as { annotations?: Record<string, string> } | undefined)?.annotations ?? {};
  const current = annotations(deployment)[key];
  return replicaSets
    .map((rs) => ({
      revision: Number(annotations(rs)[key]),
      images: getArray(rs, 'spec.template.spec.containers').map((c) => getString(c, 'image')),
    }))
    .filter((r) => Number.isInteger(r.revision) && r.revision > 0 && String(r.revision) !== current)
    .sort((a, b) => b.revision - a.revision);
}

function LogsTab({
  cluster,
  kind,
  object,
  pods,
}: {
  cluster: string;
  kind: string;
  object: K8sObject;
  pods: K8sObject[];
}) {
  const open = useDock().open;
  const targets = kind === 'Pod' ? [object] : podsOfWorkload(object, pods);
  if (targets.length === 0) return <p>No pods to show logs for.</p>;
  return (
    <ul>
      {targets.map((p) => {
        const name = getString(p, 'metadata.name');
        const containers = getArray(p, 'spec.containers')
          .map((c) => getString(c, 'name'))
          .filter((n) => n !== '');
        return (
          <li key={getString(p, 'metadata.uid') || name}>
            <Button
              onClick={() =>
                open(
                  logTab({
                    cluster,
                    namespace: getString(p, 'metadata.namespace'),
                    pod: name,
                    containers: containers.length > 0 ? containers : [''],
                  }),
                )
              }
            >{`Open logs for ${name}`}</Button>
          </li>
        );
      })}
    </ul>
  );
}

/** The right-hand drawer of one object: Overview, YAML, Events, Logs, Files and Related. */
export function DetailsDrawer({ cluster, group, kind, namespace, name, onClose }: DrawerProps) {
  const [tab, setTab] = useState<TabId>('overview');
  const state = useObject(cluster, group, kind, namespace, name);
  const object = state.state === 'found' || state.state === 'deleted' ? state.object : undefined;
  const now = useNow(MINUTE);
  const container = useFocusReturn();
  const chain = useOwnerChain(cluster, object);
  const info = useResourceInfo(cluster, group, kind);
  const update = useAccess(
    cluster,
    info ? [{ verb: 'update', group: info.group, resource: info.resource, namespace, name }] : [],
    info !== undefined,
  );

  const isCore = group === '' || group === 'core';
  const needPods = (isCore || WORKLOADS.includes(kind)) && NEEDS_PODS.includes(kind);
  const pods = useKindItems(cluster, '', 'Pod', namespace, object !== undefined && needPods);
  const replicaSets = useKindItems(
    cluster,
    'apps',
    'ReplicaSet',
    namespace,
    object !== undefined && kind === 'Deployment',
  );
  const jobs = useKindItems(cluster, 'batch', 'Job', namespace, object !== undefined && kind === 'CronJob');
  const events = useKindItems(
    cluster,
    'events.k8s.io',
    'Event',
    namespace,
    object !== undefined && tab === 'events',
  );

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const descriptor = getKind(group, kind);
  const uid = object ? getString(object, 'metadata.uid') : '';
  const children = object ? childrenOf(uid, [...pods, ...replicaSets, ...jobs]) : [];
  const tabs = tabsFor(group, kind);

  return (
    <div
      ref={container as React.RefObject<HTMLDivElement>}
      tabIndex={-1}
      aria-label={`${kind} ${name}`}
      role="region"
      className="details"
    >
      <header>
        <h2>{name}</h2>
        <span>{descriptor.title === kind ? kind : kind}</span>
        {object && descriptor.status && <StatusBadge status={descriptor.status(object)} />}
        {object && isCore && kind === 'Pod' && <OpenShell cluster={cluster} pod={object} />}
        {object && isCore && kind === 'Pod' && <DebugButton cluster={cluster} pod={object} />}
        {object && isCore && kind === 'Node' && <NodeShellButton cluster={cluster} node={object} />}
        {object && info && (
          <ActionMenu
            cluster={cluster}
            object={object}
            group={info.group}
            version={info.version}
            resource={info.resource}
            kind={kind}
            actions={descriptor.actions ?? []}
            disabled={state.state === 'deleted'}
            revisions={kind === 'Deployment' ? revisionsOf(object, childrenOf(uid, replicaSets)) : []}
          />
        )}
        <Button aria-label="Close details" onClick={onClose}>
          ×
        </Button>
      </header>
      {state.state === 'deleted' && <Banner kind="warning">This object was deleted.</Banner>}
      {state.state === 'error' && <Banner kind="error">{state.message}</Banner>}
      {state.state === 'missing' && <p>{`${kind} ${name} was not found.`}</p>}
      {state.state === 'loading' && <p role="status">Loading…</p>}
      {object && (
        <>
          <div role="tablist" aria-label="Details">
            {tabs.map((t) => (
              <button
                key={t.id}
                type="button"
                role="tab"
                aria-selected={tab === t.id}
                onClick={() => setTab(t.id)}
              >
                {t.title}
              </button>
            ))}
          </div>
          <div role="tabpanel">
            {tab === 'overview' && (
              <OverviewTab
                cluster={cluster}
                group={group}
                kind={kind}
                object={object}
                now={now}
                chain={chain}
                pods={pods}
              />
            )}
            {tab === 'yaml' && info && (
              <EditorTab
                target={{ cluster, group, version: info.version, resource: info.resource, namespace, name }}
                kind={kind}
                object={object}
                canEdit={state.state === 'found' && update.data?.[0]?.allowed === true}
              />
            )}
            {tab === 'events' && <EventsTab events={eventsFor(uid, events)} now={now} />}
            {tab === 'logs' && <LogsTab cluster={cluster} kind={kind} object={object} pods={pods} />}
            {tab === 'files' && <p>The file browser opens here.</p>}
            {tab === 'related' && (
              <RelatedTab
                cluster={cluster}
                kind={kind}
                object={object}
                chain={chain}
                children={children}
                pods={pods}
              />
            )}
          </div>
        </>
      )}
    </div>
  );
}
