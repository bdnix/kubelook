import { useState } from 'react';
import { Button } from '../../components/Button';
import { knownKinds } from '../../kinds/registry';
import { applyPreset, movePreset, presetKey, togglePreset } from './columnPreset';

export interface ColumnPresetsProps {
  presets: Record<string, string[]>;
  onChange: (presets: Record<string, string[]>) => void;
}

/** Chooses, per kind, which columns the lists show and in which order. */
export function ColumnPresets({ presets, onChange }: ColumnPresetsProps) {
  const kinds = knownKinds();
  const [selected, setSelected] = useState('');
  const kind = kinds.find((k) => presetKey(k.group, k.kind) === selected);
  const key = kind ? presetKey(kind.group, kind.kind) : '';
  const all = kind?.columns.map((c) => c.id) ?? [];
  const preset = presets[key];
  const visible = applyPreset(kind?.columns ?? [], preset).map((c) => c.id);
  const hidden = all.filter((id) => !visible.includes(id));
  const title = (id: string) => kind?.columns.find((c) => c.id === id)?.title ?? id;
  const set = (ids: string[]) => onChange({ ...presets, [key]: ids });
  const reset = () => {
    const { [key]: _removed, ...rest } = presets;
    void _removed;
    onChange(rest);
  };

  return (
    <div className="column-presets">
      <label>
        Kind
        <select value={selected} onChange={(e) => setSelected(e.target.value)}>
          <option value="">Choose a kind</option>
          {kinds.map((k) => (
            <option key={presetKey(k.group, k.kind)} value={presetKey(k.group, k.kind)}>
              {k.title}
            </option>
          ))}
        </select>
      </label>
      {kind && (
        <>
          <ul aria-label="Columns">
            {[...visible, ...hidden].map((id) => {
              const shown = visible.includes(id);
              return (
                <li key={id}>
                  <label>
                    <input
                      type="checkbox"
                      checked={shown}
                      onChange={() => set(togglePreset(all, preset, id))}
                    />
                    {title(id)}
                  </label>
                  {shown && (
                    <>
                      <Button
                        aria-label={`Move ${title(id)} up`}
                        onClick={() => set(movePreset(all, preset, id, -1))}
                      >
                        ↑
                      </Button>
                      <Button
                        aria-label={`Move ${title(id)} down`}
                        onClick={() => set(movePreset(all, preset, id, 1))}
                      >
                        ↓
                      </Button>
                    </>
                  )}
                </li>
              );
            })}
          </ul>
          <Button disabled={preset === undefined} onClick={reset}>
            Reset columns
          </Button>
        </>
      )}
    </div>
  );
}
