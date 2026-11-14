import { formatAge } from '../../lib/age';
import { getString } from '../../lib/path';

/** The `status.conditions` of an object as a table. Renders nothing when there are none. */
export function Conditions({ conditions, now }: { conditions: unknown[]; now: number }) {
  if (conditions.length === 0) return null;
  return (
    <table aria-label="Conditions">
      <thead>
        <tr>
          <th>Type</th>
          <th>Status</th>
          <th>Reason</th>
          <th>Message</th>
          <th>Last transition</th>
        </tr>
      </thead>
      <tbody>
        {conditions.map((c, i) => (
          <tr key={`${getString(c, 'type')}-${i}`}>
            <td>{getString(c, 'type')}</td>
            <td>{getString(c, 'status')}</td>
            <td>{getString(c, 'reason')}</td>
            <td>{getString(c, 'message')}</td>
            <td>{formatAge(getString(c, 'lastTransitionTime'), now)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
