import { Link } from 'react-router';
import { getString } from '../../lib/path';
import { resourcePath } from '../../lib/paths';
import type { K8sObject } from '../../state/live';
import { severityTotals, topReports } from './findings';

export interface ReportRef {
  kind: string;
  group: string;
  object: K8sObject;
}

const counts = (o: K8sObject) =>
  (o.report as { summary?: Record<string, number> } | undefined)?.summary ?? {};

/** The totals per severity and the 20 worst reports. */
export function TrivyOverviewView({ cluster, reports }: { cluster: string; reports: ReportRef[] }) {
  const totals = severityTotals(reports.map((r) => r.object));
  const worst = topReports(reports.map((r) => r.object));
  const ref = (o: K8sObject) => reports.find((r) => r.object === o);
  return (
    <>
      <section aria-label="Totals">
        <ul>
          <li>
            Critical <strong>{totals.critical}</strong>
          </li>
          <li>
            High <strong>{totals.high}</strong>
          </li>
          <li>
            Medium <strong>{totals.medium}</strong>
          </li>
          <li>
            Low <strong>{totals.low}</strong>
          </li>
        </ul>
      </section>
      <ul aria-label="Worst reports">
        {worst.map((o) => {
          const r = ref(o);
          const name = getString(o, 'metadata.name');
          const c = counts(o);
          return (
            <li key={`${r?.kind}/${getString(o, 'metadata.namespace')}/${name}`}>
              <Link
                to={resourcePath({
                  cluster,
                  namespace: getString(o, 'metadata.namespace'),
                  group: r?.group ?? '',
                  kind: r?.kind ?? '',
                  name,
                })}
              >
                {name}
              </Link>{' '}
              {`${r?.kind ?? ''}: ${c.criticalCount ?? 0} critical, ${c.highCount ?? 0} high`}
            </li>
          );
        })}
      </ul>
    </>
  );
}
