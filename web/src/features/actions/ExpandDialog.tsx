import { useState } from 'react';
import { Field } from '../../components/Field';
import { kubectlCommand } from '../editor/kubectlCommand';
import { ConfirmDialog, type Clipboard } from './ConfirmDialog';

interface Props {
  name: string;
  namespace: string;
  current: string;
  busy: boolean;
  error?: string;
  onConfirm: (storage: string) => void;
  onCancel: () => void;
  clipboard?: Clipboard;
}

/** Asks for the new size of a volume claim. The server checks that it is larger and allowed. */
export function ExpandDialog({
  name,
  namespace,
  current,
  busy,
  error,
  onConfirm,
  onCancel,
  clipboard,
}: Props) {
  const [size, setSize] = useState(current);
  const valid = /^\d+(\.\d+)?(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?$/.test(size);
  return (
    <ConfirmDialog
      title={`Expand volume claim ${name}`}
      confirmLabel="Expand"
      busy={busy}
      error={error}
      canConfirm={valid && size !== current}
      kubectl={kubectlCommand({ type: 'expandPVC', name, storage: size, namespace })}
      onConfirm={() => onConfirm(size)}
      onCancel={onCancel}
      clipboard={clipboard}
    >
      <Field
        label="New size"
        value={size}
        onChange={(e) => setSize(e.target.value)}
        hint={`Now ${current}. Examples: 20Gi, 500Mi.`}
      />
    </ConfirmDialog>
  );
}
