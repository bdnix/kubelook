import { useState } from 'react';
import { Field } from '../../components/Field';
import { kubectlCommand } from '../editor/kubectlCommand';
import { ConfirmDialog, type Clipboard } from './ConfirmDialog';

export type BulkAction = 'delete' | 'restart' | 'label' | 'cordon' | 'uncordon' | 'drain';

export interface LabelParams {
  add: Record<string, string>;
  remove: string[];
}

export const EMPTY_DIR_NOTE =
  'Pods with emptyDir volumes are evicted too and their data is lost. Pods of DaemonSets and mirror pods stay.';

const VERBS: Record<BulkAction, string> = {
  delete: 'Delete',
  restart: 'Restart',
  label: 'Label',
  cordon: 'Cordon',
  uncordon: 'Uncordon',
  drain: 'Drain',
};

export const bulkVerb = (a: BulkAction) => VERBS[a];

/** Parses `a=1, b=2` into labels; undefined when an entry has no key or no equals sign. */
export function parseLabels(text: string): Record<string, string> | undefined {
  const out: Record<string, string> = {};
  for (const part of text
    .split(',')
    .map((p) => p.trim())
    .filter((p) => p !== '')) {
    const i = part.indexOf('=');
    if (i <= 0) return undefined;
    out[part.slice(0, i).trim()] = part.slice(i + 1).trim();
  }
  return out;
}

export interface BulkConfirmProps {
  action: BulkAction;
  /** Every object, as `namespace/name` or `name`. */
  names: string[];
  kind: string;
  busy: boolean;
  error?: string;
  onConfirm: (params: LabelParams | undefined) => void;
  onCancel: () => void;
  clipboard?: Clipboard;
}

function commandFor(action: BulkAction, kind: string, name: string, labels: LabelParams): string {
  const slash = name.indexOf('/');
  const namespace = slash >= 0 ? name.slice(0, slash) : undefined;
  const bare = slash >= 0 ? name.slice(slash + 1) : name;
  const resource = kind.toLowerCase();
  switch (action) {
    case 'delete':
      return kubectlCommand({ type: 'delete', resource, name: bare, namespace });
    case 'restart':
      return kubectlCommand({ type: 'restart', resource, name: bare, namespace });
    case 'label':
      return kubectlCommand({ type: 'label', resource, name: bare, namespace, ...labels });
    default:
      return kubectlCommand({ type: action, name: bare });
  }
}

/** Names every object a bulk action will change and waits for an explicit confirm. */
export function BulkConfirm({
  action,
  names,
  kind,
  busy,
  error,
  onConfirm,
  onCancel,
  clipboard,
}: BulkConfirmProps) {
  const [addText, setAddText] = useState('');
  const [removeText, setRemoveText] = useState('');
  const n = names.length;
  const verb = VERBS[action];
  const add = parseLabels(addText);
  const remove = removeText
    .split(',')
    .map((p) => p.trim())
    .filter((p) => p !== '');
  const labels: LabelParams = { add: add ?? {}, remove };
  const labelsOk = add !== undefined && (Object.keys(add).length > 0 || remove.length > 0);
  const canConfirm = action !== 'label' || labelsOk;
  return (
    <ConfirmDialog
      title={`${verb} ${n} ${kind}${n === 1 ? '' : 's'}`}
      confirmLabel={`${verb} ${n} object${n === 1 ? '' : 's'}`}
      destructive={action === 'delete' || action === 'drain'}
      busy={busy}
      error={error}
      canConfirm={canConfirm}
      kubectl={names.map((name) => commandFor(action, kind, name, labels)).join('\n')}
      onConfirm={() => onConfirm(action === 'label' ? labels : undefined)}
      onCancel={onCancel}
      clipboard={clipboard}
    >
      {action === 'drain' && (
        <>
          <p>Each node is cordoned first and then its pods are evicted, one node at a time.</p>
          <p>{EMPTY_DIR_NOTE}</p>
        </>
      )}
      {action === 'label' && (
        <>
          <Field
            label="Labels to add"
            value={addText}
            onChange={(e) => setAddText(e.target.value)}
            hint={add === undefined ? 'Write labels as key=value, separated by commas.' : undefined}
          />
          <Field
            label="Labels to remove"
            value={removeText}
            onChange={(e) => setRemoveText(e.target.value)}
          />
        </>
      )}
      <ul className="bulk-names">
        {names.map((name) => (
          <li key={name}>{name}</li>
        ))}
      </ul>
    </ConfirmDialog>
  );
}
