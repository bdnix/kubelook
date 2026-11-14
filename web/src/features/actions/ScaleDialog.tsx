import { useState } from 'react';
import { Field } from '../../components/Field';
import { kubectlCommand } from '../editor/kubectlCommand';
import { ConfirmDialog, type Clipboard } from './ConfirmDialog';

interface Props {
  kind: string;
  resource: string;
  name: string;
  namespace: string;
  current: number;
  busy: boolean;
  error?: string;
  onConfirm: (replicas: number) => void;
  onCancel: () => void;
  clipboard?: Clipboard;
}

/** Asks for the number of replicas. */
export function ScaleDialog({
  kind,
  resource,
  name,
  namespace,
  current,
  busy,
  error,
  onConfirm,
  onCancel,
  clipboard,
}: Props) {
  const [text, setText] = useState(String(current));
  const replicas = Number(text);
  const valid = text.trim() !== '' && Number.isInteger(replicas) && replicas >= 0;
  return (
    <ConfirmDialog
      title={`Scale ${kind} ${name}`}
      confirmLabel="Scale"
      busy={busy}
      error={error}
      canConfirm={valid}
      kubectl={kubectlCommand({
        type: 'scale',
        resource: resource.replace(/s$/, ''),
        name,
        replicas: valid ? replicas : current,
        namespace,
      })}
      onConfirm={() => onConfirm(replicas)}
      onCancel={onCancel}
      clipboard={clipboard}
    >
      <Field
        label="Replicas"
        type="number"
        min={0}
        value={text}
        onChange={(e) => setText(e.target.value)}
        hint={valid ? undefined : 'Enter zero or a positive whole number.'}
      />
    </ConfirmDialog>
  );
}
