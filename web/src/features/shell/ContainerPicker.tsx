import { getArray, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';

export interface ShellContainer {
  name: string;
  kind: 'container' | 'init' | 'ephemeral';
}

const running = (status: unknown) =>
  (status as { state?: { running?: unknown } } | null)?.state?.running !== undefined;

/** The containers of a pod a shell can start in: running ones, init containers and ephemeral containers included. */
export function runningContainers(pod: K8sObject): ShellContainer[] {
  const pick = (path: string, kind: ShellContainer['kind']) =>
    getArray(pod, path)
      .filter(running)
      .map((s) => ({ name: getString(s, 'name'), kind }));
  return [
    ...pick('status.containerStatuses', 'container'),
    ...pick('status.initContainerStatuses', 'init'),
    ...pick('status.ephemeralContainerStatuses', 'ephemeral'),
  ];
}

const SUFFIX: Record<ShellContainer['kind'], string> = {
  container: '',
  init: ' (init)',
  ephemeral: ' (debug)',
};

export function ContainerPicker({
  containers,
  value,
  onChange,
}: {
  containers: ShellContainer[];
  value: string;
  onChange: (name: string) => void;
}) {
  return (
    <label>
      Container
      <select value={value} onChange={(e) => onChange(e.target.value)}>
        {containers.map((c) => (
          <option key={`${c.kind}-${c.name}`} value={c.name}>
            {`${c.name}${SUFFIX[c.kind]}`}
          </option>
        ))}
      </select>
    </label>
  );
}
