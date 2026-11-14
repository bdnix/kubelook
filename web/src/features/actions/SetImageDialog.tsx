import { useState } from 'react';
import { Field } from '../../components/Field';
import { kubectlCommand } from '../editor/kubectlCommand';
import { ConfirmDialog, type Clipboard } from './ConfirmDialog';

export interface ContainerChoice {
  name: string;
  image: string;
  init: boolean;
}

interface Props {
  kind: string;
  resource: string;
  name: string;
  namespace: string;
  containers: ContainerChoice[];
  busy: boolean;
  error?: string;
  onConfirm: (p: { container: string; image: string; init: boolean }) => void;
  onCancel: () => void;
  clipboard?: Clipboard;
}

/** Sets the image of a container or init container. */
export function SetImageDialog({
  kind,
  resource,
  name,
  namespace,
  containers,
  busy,
  error,
  onConfirm,
  onCancel,
  clipboard,
}: Props) {
  const [index, setIndex] = useState(0);
  const chosen = containers[index];
  const [image, setImage] = useState<string | undefined>(undefined);
  const value = image ?? chosen?.image ?? '';
  const valid = chosen !== undefined && value !== '' && !/\s/.test(value);
  return (
    <ConfirmDialog
      title={`Set image of ${kind} ${name}`}
      confirmLabel="Set image"
      busy={busy}
      error={error}
      canConfirm={valid}
      kubectl={kubectlCommand({
        type: 'setImage',
        resource: resource.replace(/s$/, ''),
        name,
        container: chosen?.name ?? '',
        image: value,
        namespace,
      })}
      onConfirm={() => chosen && onConfirm({ container: chosen.name, image: value, init: chosen.init })}
      onCancel={onCancel}
      clipboard={clipboard}
    >
      <label>
        Container
        <select
          value={index}
          onChange={(e) => {
            setIndex(Number(e.target.value));
            setImage(undefined);
          }}
        >
          {containers.map((c, i) => (
            <option key={`${c.init}-${c.name}`} value={i}>
              {c.init ? `${c.name} (init)` : c.name}
            </option>
          ))}
        </select>
      </label>
      <Field
        label="Image"
        value={value}
        onChange={(e) => setImage(e.target.value)}
        hint={valid ? undefined : 'Enter an image without spaces.'}
      />
    </ConfirmDialog>
  );
}
