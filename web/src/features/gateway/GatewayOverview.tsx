import { formatAge } from '../../lib/age';
import { getArray, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';

export type GatewayState = 'ready' | 'notReady' | 'inProgress' | 'unknown';
const TYPES = ['Accepted', 'Programmed', 'Ready'];

/** Any False condition is Not Ready, any Unknown is In Progress, all True is Ready, none of them is Unknown. */
export function gatewayState(o: K8sObject): GatewayState {
  const seen = getArray(o, 'status.conditions').filter((c) => TYPES.includes(getString(c, 'type')));
  if (seen.length === 0) return 'unknown';
  if (seen.some((c) => getString(c, 'status') === 'False')) return 'notReady';
  if (seen.some((c) => getString(c, 'status') !== 'True')) return 'inProgress';
  return 'ready';
}

const when = (e: K8sObject) =>
  getString(e, 'lastTimestamp') || getString(e, 'eventTime') || getString(e, 'metadata.creationTimestamp');

/** Counts per kind and state, and the events of the Gateway API objects. */
export function GatewayOverview({
  kinds,
  events,
  now,
}: {
  kinds: { kind: string; objects: K8sObject[] }[];
  events: K8sObject[];
  now: number;
}) {
  if (kinds.length === 0) return <p>This cluster serves no Gateway API kinds.</p>;
  return (
    <>
      <table aria-label="Gateway API objects">
        <thead>
          <tr>
            {['Kind', 'Ready', 'Not Ready', 'In Progress', 'Unknown'].map((h) => (
              <th key={h}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {kinds.map(({ kind, objects }) => {
            const count = (s: GatewayState) => objects.filter((o) => gatewayState(o) === s).length;
            return (
              <tr key={kind}>
                <td>{kind}</td>
                <td>{count('ready')}</td>
                <td>{count('notReady')}</td>
                <td>{count('inProgress')}</td>
                <td>{count('unknown')}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <ul aria-label="Events">
        {events.map((e, i) => (
          <li key={i}>
            <strong>{getString(e, 'reason')}</strong> {getString(e, 'message') || getString(e, 'note')}{' '}
            {formatAge(when(e), now)}
          </li>
        ))}
      </ul>
    </>
  );
}
