import { Link } from 'react-router';
import { clusterPath } from '../../lib/paths';
import type { Controller, ServedFluxKind, Summary } from './summary';

export interface DashboardRow {
  kind: ServedFluxKind;
  summary: Summary;
}

/** Ready, failed, in-progress and suspended counts per Flux kind, and the health of the controllers. */
export function FluxDashboardView({
  cluster,
  rows,
  controllers,
}: {
  cluster: string;
  rows: DashboardRow[];
  controllers: Controller[];
}) {
  const version = controllers.find((c) => c.version !== '')?.version;
  return (
    <>
      <h2>{version ? `Flux ${version}` : 'Flux'}</h2>
      {controllers.length === 0 ? (
        <p>No Flux controllers were found (Deployments labelled app.kubernetes.io/part-of=flux).</p>
      ) : (
        <ul aria-label="Controllers">
          {controllers.map((c) => (
            <li key={c.name}>
              <strong>{c.name}</strong> {c.ready ? 'healthy' : `${c.available} of ${c.desired} available`}
            </li>
          ))}
        </ul>
      )}
      {rows.length === 0 ? (
        <p>This cluster serves no Flux kinds.</p>
      ) : (
        <table aria-label="Flux objects">
          <thead>
            <tr>
              {['Kind', 'Total', 'Ready', 'Failed', 'In progress', 'Suspended'].map((h) => (
                <th key={h}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map(({ kind, summary }) => (
              <tr key={`${kind.group}/${kind.def.kind}`}>
                <td>
                  <Link to={`${clusterPath(cluster)}/${kind.group}/${kind.def.kind}`}>{kind.def.kind}</Link>
                </td>
                <td>{summary.total}</td>
                <td>{summary.reportsStatus ? summary.ready : '–'}</td>
                <td>{summary.reportsStatus ? summary.failed : '–'}</td>
                <td>{summary.reportsStatus ? summary.inProgress : '–'}</td>
                <td>{summary.suspended}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
