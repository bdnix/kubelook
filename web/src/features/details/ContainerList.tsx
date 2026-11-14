import { get, getArray, getNumber, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';

function state(status: unknown): string {
  if (get(status, 'state.running') !== undefined) return 'Running';
  const waiting = getString(status, 'state.waiting.reason');
  if (get(status, 'state.waiting') !== undefined) return waiting ? `Waiting: ${waiting}` : 'Waiting';
  const terminated = getString(status, 'state.terminated.reason');
  if (get(status, 'state.terminated') !== undefined)
    return terminated ? `Terminated: ${terminated}` : 'Terminated';
  return '';
}

function ports(container: unknown): string {
  return getArray(container, 'ports')
    .map((p) => `${getNumber(p, 'containerPort')}/${getString(p, 'protocol') || 'TCP'}`)
    .join(', ');
}

function Table({
  title,
  containers,
  statuses,
}: {
  title: string;
  containers: unknown[];
  statuses: unknown[];
}) {
  if (containers.length === 0) return null;
  return (
    <table aria-label={title}>
      <thead>
        <tr>
          <th>Name</th>
          <th>Image</th>
          <th>State</th>
          <th>Restarts</th>
          <th>Ports</th>
          <th>Last termination</th>
        </tr>
      </thead>
      <tbody>
        {containers.map((c) => {
          const name = getString(c, 'name');
          const status = statuses.find((s) => getString(s, 'name') === name);
          return (
            <tr key={name}>
              <td>{name}</td>
              <td>{getString(c, 'image')}</td>
              <td>{state(status)}</td>
              <td>{getNumber(status, 'restartCount')}</td>
              <td>{ports(c)}</td>
              <td>{getString(status, 'lastState.terminated.reason')}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

/** The containers and init containers of a pod, with image, state, restarts, ports and last termination. */
export function ContainerList({ pod }: { pod: K8sObject }) {
  return (
    <>
      <Table
        title="Containers"
        containers={getArray(pod, 'spec.containers')}
        statuses={getArray(pod, 'status.containerStatuses')}
      />
      <Table
        title="Init containers"
        containers={getArray(pod, 'spec.initContainers')}
        statuses={getArray(pod, 'status.initContainerStatuses')}
      />
    </>
  );
}
