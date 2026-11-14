import { formatAge } from '../../lib/age';
import { getNumber, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';

function when(e: K8sObject): string {
  return (
    getString(e, 'series.lastObservedTime') ||
    getString(e, 'eventTime') ||
    getString(e, 'deprecatedLastTimestamp') ||
    getString(e, 'lastTimestamp') ||
    getString(e, 'metadata.creationTimestamp')
  );
}

/** The events of the object, newest first (the caller filters and sorts). */
export function EventsTab({ events, now }: { events: K8sObject[]; now: number }) {
  if (events.length === 0) return <p>No events.</p>;
  return (
    <table aria-label="Events">
      <thead>
        <tr>
          <th>Type</th>
          <th>Reason</th>
          <th>Message</th>
          <th>Count</th>
          <th>Last seen</th>
        </tr>
      </thead>
      <tbody>
        {events.map((e) => (
          <tr key={getString(e, 'metadata.uid') || getString(e, 'metadata.name')}>
            <td>{getString(e, 'type')}</td>
            <td>{getString(e, 'reason')}</td>
            <td>{getString(e, 'note') || getString(e, 'message')}</td>
            <td>
              {getNumber(e, 'series.count') || getNumber(e, 'deprecatedCount') || getNumber(e, 'count', 1)}
            </td>
            <td>{formatAge(when(e), now)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
