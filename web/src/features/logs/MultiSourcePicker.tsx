import { useState } from 'react';
import { Button } from '../../components/Button';

/** What a log channel follows; the server's `sources`. */
export interface LogSources {
  pod?: string;
  container?: string;
  allContainers?: boolean;
  selector?: string;
  workload?: { group: string; kind: string; name: string };
}

const WORKLOAD_KINDS: Array<[string, string]> = [
  ['Deployment', 'apps'],
  ['StatefulSet', 'apps'],
  ['DaemonSet', 'apps'],
  ['ReplicaSet', 'apps'],
  ['Job', 'batch'],
];

export const MAX_STREAMS = 20;

type Mode = string; // container:<name> | all | selector | workload

function modeOf(v: LogSources): Mode {
  if (v.workload) return 'workload';
  if (v.selector !== undefined) return 'selector';
  if (v.allContainers) return 'all';
  return `container:${v.container ?? ''}`;
}

export interface MultiSourcePickerProps {
  pod: string;
  containers: string[];
  value: LogSources;
  onChange: (value: LogSources) => void;
}

/** Chooses what to follow: one container, every container of the pod, pods by label selector, or a workload's pods. */
export function MultiSourcePicker({ pod, containers, value, onChange }: MultiSourcePickerProps) {
  const [mode, setMode] = useState<Mode>(modeOf(value));
  const [selector, setSelector] = useState(value.selector ?? '');
  const [kind, setKind] = useState(value.workload?.kind ?? 'Deployment');
  const [name, setName] = useState(value.workload?.name ?? '');

  const choose = (next: Mode) => {
    setMode(next);
    if (next === 'all') onChange({ pod, allContainers: true });
    else if (next.startsWith('container:')) onChange({ pod, container: next.slice('container:'.length) });
  };
  const group = WORKLOAD_KINDS.find(([k]) => k === kind)?.[1] ?? 'apps';
  const apply = () => {
    if (mode === 'selector') onChange({ selector: selector.trim() });
    else onChange({ workload: { group, kind, name: name.trim() } });
  };
  const ready = mode === 'selector' ? selector.trim() !== '' : name.trim() !== '';

  return (
    <span className="source-picker">
      <label>
        Source
        <select value={mode} onChange={(e) => choose(e.target.value)}>
          {containers.map((c) => (
            <option key={c} value={`container:${c}`}>
              {c}
            </option>
          ))}
          <option value="all">All containers</option>
          <option value="selector">Pods matching a label selector</option>
          <option value="workload">Pods of a workload</option>
        </select>
      </label>
      {mode === 'selector' && (
        <label>
          Label selector
          <input
            value={selector}
            onChange={(e) => setSelector(e.target.value)}
            placeholder="app=web,tier=frontend"
          />
        </label>
      )}
      {mode === 'workload' && (
        <>
          <label>
            Workload kind
            <select value={kind} onChange={(e) => setKind(e.target.value)}>
              {WORKLOAD_KINDS.map(([k]) => (
                <option key={k} value={k}>
                  {k}
                </option>
              ))}
            </select>
          </label>
          <label>
            Workload name
            <input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
        </>
      )}
      {(mode === 'selector' || mode === 'workload') && (
        <Button disabled={!ready} onClick={apply}>
          Follow
        </Button>
      )}
      {mode !== '' && !mode.startsWith('container:') && (
        <small>{`A selection follows at most ${MAX_STREAMS} containers.`}</small>
      )}
    </span>
  );
}
