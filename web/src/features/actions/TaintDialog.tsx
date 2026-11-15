import { useEffect, useRef, useState } from 'react';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { Field } from '../../components/Field';
import { useFocusTrap } from '../../lib/useFocusTrap';

export interface Taint {
  key: string;
  value?: string;
  effect: string;
}

export const TAINT_EFFECTS = ['NoSchedule', 'PreferNoSchedule', 'NoExecute'];

export interface TaintDialogProps {
  nodeName: string;
  taints: Taint[];
  busy: boolean;
  error?: string;
  onAdd: (t: { key: string; value: string; effect: string }) => void;
  onRemove: (t: { key: string; effect: string }) => void;
  onClose: () => void;
}

const describe = (t: Taint) => `${t.key}${t.value ? `=${t.value}` : ''}:${t.effect}`;

/** Lists the taints of a node and adds or removes one. A key and effect that exist already are refused at once. */
export function TaintDialog({ nodeName, taints, busy, error, onAdd, onRemove, onClose }: TaintDialogProps) {
  const [key, setKey] = useState('');
  const [value, setValue] = useState('');
  const [effect, setEffect] = useState(TAINT_EFFECTS[0] as string);
  const ref = useRef<HTMLDivElement>(null);
  useFocusTrap(ref);
  useEffect(() => ref.current?.focus(), []);
  const duplicate = key !== '' && taints.some((t) => t.key === key && t.effect === effect);
  return (
    <div
      ref={ref}
      role="dialog"
      aria-modal="true"
      aria-label={`Taints of ${nodeName}`}
      tabIndex={-1}
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.stopPropagation();
          onClose();
        }
      }}
    >
      <h2>{`Taints of ${nodeName}`}</h2>
      {taints.length === 0 ? (
        <p>This node has no taints.</p>
      ) : (
        <ul>
          {taints.map((t) => (
            <li key={`${t.key}:${t.effect}`}>
              <span>{describe(t)}</span>{' '}
              <Button
                aria-label={`Remove ${describe(t)}`}
                disabled={busy}
                onClick={() => onRemove({ key: t.key, effect: t.effect })}
              >
                Remove
              </Button>
            </li>
          ))}
        </ul>
      )}
      <Field label="Key" value={key} onChange={(e) => setKey(e.target.value)} />
      <Field label="Value (optional)" value={value} onChange={(e) => setValue(e.target.value)} />
      <div className="field">
        <label htmlFor="taint-effect">Effect</label>
        <select id="taint-effect" value={effect} onChange={(e) => setEffect(e.target.value)}>
          {TAINT_EFFECTS.map((x) => (
            <option key={x} value={x}>
              {x}
            </option>
          ))}
        </select>
      </div>
      {duplicate && <p role="status">The node already has this taint.</p>}
      <ErrorText message={error} />
      <Button onClick={onClose}>Close</Button>
      <Button
        disabled={busy || key.trim() === '' || duplicate}
        onClick={() => onAdd({ key: key.trim(), value, effect })}
      >
        Add taint
      </Button>
    </div>
  );
}
