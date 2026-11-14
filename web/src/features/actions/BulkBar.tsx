import { useState } from 'react';
import { ApiError } from '../../api/client';
import { useApi } from '../../api/context';
import { Button } from '../../components/Button';
import { getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { useJobs, useRefreshJobs } from '../jobs/useJobs';
import { BulkConfirm, bulkVerb, type BulkAction, type LabelParams } from './BulkConfirm';
import type { Clipboard } from './ConfirmDialog';

export const MAX_BULK = 500;

const WORKLOADS = ['Deployment', 'StatefulSet', 'DaemonSet'];

/** The bulk actions that make sense for a kind, in the order of the bar. */
export function bulkActionsFor(group: string, kind: string): BulkAction[] {
  if (group === '' && kind === 'Node') return ['cordon', 'uncordon', 'drain', 'label', 'delete'];
  if (group === 'apps' && WORKLOADS.includes(kind)) return ['restart', 'label', 'delete'];
  return ['label', 'delete'];
}

const LABELS: Partial<Record<BulkAction, string>> = { label: 'Label…', drain: 'Drain…' };

export interface BulkBarProps {
  cluster: string;
  group: string;
  version: string;
  resource: string;
  kind: string;
  /** The selected objects. */
  objects: K8sObject[];
  /** Called after the job has been started. */
  onDone?: () => void;
  clipboard?: Clipboard;
}

/** Appears above a list while rows are selected: one button per action valid for the kind. */
export function BulkBar({
  cluster,
  group,
  version,
  resource,
  kind,
  objects,
  onDone,
  clipboard,
}: BulkBarProps) {
  const api = useApi();
  const refresh = useRefreshJobs();
  const jobs = useJobs();
  const [active, setActive] = useState<BulkAction>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [jobId, setJobId] = useState<string>();
  if (objects.length === 0) return null;

  const tooMany = objects.length > MAX_BULK;
  const names = objects.map((o) => {
    const ns = getString(o, 'metadata.namespace');
    const name = getString(o, 'metadata.name');
    return ns ? `${ns}/${name}` : name;
  });
  const job = jobs.data?.find((j) => j.id === jobId);

  const close = () => {
    setActive(undefined);
    setError(undefined);
  };
  const start = async (action: BulkAction, params: LabelParams | undefined) => {
    setBusy(true);
    setError(undefined);
    try {
      const res = await api.post<{ jobId: string }>(`/clusters/${encodeURIComponent(cluster)}/bulk`, {
        action,
        targets: objects.map((o) => ({
          group: group === '' ? 'core' : group,
          version,
          resource,
          namespace: getString(o, 'metadata.namespace'),
          name: getString(o, 'metadata.name'),
        })),
        params,
      });
      setJobId(res.jobId);
      close();
      void refresh();
      onDone?.();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Something went wrong.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div role="toolbar" aria-label="Bulk actions" className="bulk-bar">
      <span>{`${objects.length} selected`}</span>
      {bulkActionsFor(group, kind).map((a) => (
        <Button key={a} disabled={tooMany} onClick={() => setActive(a)}>
          {LABELS[a] ?? bulkVerb(a)}
        </Button>
      ))}
      {tooMany && <span role="status">{`Select at most ${MAX_BULK} objects.`}</span>}
      {job && (
        <span role="status" className="bulk-progress">
          {job.progress.text || job.status}
        </span>
      )}
      {active && (
        <BulkConfirm
          action={active}
          names={names}
          kind={kind}
          busy={busy}
          error={error}
          onConfirm={(params) => void start(active, params)}
          onCancel={close}
          clipboard={clipboard}
        />
      )}
    </div>
  );
}
