import { Link } from 'react-router';
import { getString } from '../../lib/path';
import { resourcePath } from '../../lib/paths';
import type { ReportRef } from './TrivyOverview';

/** The Trivy reports about a workload, with their severity counts. Renders nothing without reports. */
export function WorkloadTrivySection({ cluster, reports }: { cluster: string; reports: ReportRef[] }) {
  if (reports.length === 0) return null;
  return (
    <section aria-label="Trivy">
      <h3>Trivy</h3>
      <ul>
        {reports.map((r) => {
          const s = (r.object.report as { summary?: Record<string, number> } | undefined)?.summary ?? {};
          const name = getString(r.object, 'metadata.name');
          return (
            <li key={`${r.kind}/${name}`}>
              {`${r.kind}: `}
              <Link
                to={resourcePath({
                  cluster,
                  namespace: getString(r.object, 'metadata.namespace'),
                  group: r.group,
                  kind: r.kind,
                  name,
                })}
              >
                {name}
              </Link>
              {` ${s.criticalCount ?? 0} critical, ${s.highCount ?? 0} high, ${s.mediumCount ?? 0} medium, ${s.lowCount ?? 0} low`}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
