import { useState } from 'react';
import { kubectlCommand } from '../editor/kubectlCommand';
import { ConfirmDialog, type Clipboard } from './ConfirmDialog';

export interface Revision {
  revision: number;
  images: string[];
}

interface Props {
  name: string;
  namespace: string;
  /** The revisions kept as ReplicaSets, newest first; the current one is excluded by the caller. */
  revisions: Revision[];
  busy: boolean;
  error?: string;
  onConfirm: (revision: number) => void;
  onCancel: () => void;
  clipboard?: Clipboard;
}

/** Picks the revision of a Deployment to roll back to. */
export function RollbackDialog({
  name,
  namespace,
  revisions,
  busy,
  error,
  onConfirm,
  onCancel,
  clipboard,
}: Props) {
  const [revision, setRevision] = useState(revisions[0]?.revision);
  return (
    <ConfirmDialog
      title={`Roll back Deployment ${name}`}
      confirmLabel="Roll back"
      destructive
      busy={busy}
      error={error}
      canConfirm={revision !== undefined}
      kubectl={kubectlCommand({
        type: 'rollback',
        resource: 'deployment',
        name,
        revision: revision ?? 0,
        namespace,
      })}
      onConfirm={() => revision !== undefined && onConfirm(revision)}
      onCancel={onCancel}
      clipboard={clipboard}
    >
      {revisions.length === 0 ? (
        <p>There is no earlier revision to roll back to.</p>
      ) : (
        <label>
          Revision
          <select value={revision} onChange={(e) => setRevision(Number(e.target.value))}>
            {revisions.map((r) => (
              <option key={r.revision} value={r.revision}>
                {`${r.revision}: ${r.images.join(', ')}`}
              </option>
            ))}
          </select>
        </label>
      )}
    </ConfirmDialog>
  );
}
