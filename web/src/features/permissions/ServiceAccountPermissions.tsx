import { Link } from 'react-router';
import { Banner } from '../../components/Banner';
import { getString } from '../../lib/path';
import { resourcePath } from '../../lib/paths';
import type { RbacData } from '../../lib/rbac';
import { effectiveRules, riskLevel, type Risk } from '../../lib/risk';
import type { K8sObject } from '../../state/live';
import { useLive } from '../../state/useLive';
import { useRbacData } from '../rbac/useRbacData';

const ORDER: Risk[] = ['critical', 'high', 'medium', 'low'];

export interface PermissionsViewProps {
  cluster: string;
  accounts: K8sObject[];
  data: RbacData;
  forbidden: string[];
  truncated: string[];
}

/** Every ServiceAccount with the risk of the rules it holds, riskiest first. Read only. */
export function ServiceAccountPermissionsView({
  cluster,
  accounts,
  data,
  forbidden,
  truncated,
}: PermissionsViewProps) {
  const rows = accounts
    .map((a) => {
      const name = getString(a, 'metadata.name');
      const namespace = getString(a, 'metadata.namespace');
      return { name, namespace, ...riskLevel(effectiveRules({ name, namespace }, data)) };
    })
    .sort(
      (a, b) =>
        ORDER.indexOf(a.level) - ORDER.indexOf(b.level) ||
        a.namespace.localeCompare(b.namespace) ||
        a.name.localeCompare(b.name),
    );
  return (
    <section aria-label="ServiceAccount permissions">
      <h2>ServiceAccount permissions</h2>
      <p>Based on RBAC rules only.</p>
      {forbidden.length > 0 && (
        <Banner kind="info">{`You may not list ${forbidden.join(', ')}, so the answer is incomplete.`}</Banner>
      )}
      {truncated.length > 0 && (
        <Banner kind="info">{`${truncated.join(', ')} was cut off at the limit, so the answer is incomplete.`}</Banner>
      )}
      <table aria-label="ServiceAccounts">
        <thead>
          <tr>
            <th>Risk</th>
            <th>ServiceAccount</th>
            <th>Namespace</th>
            <th>Why</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={`${r.namespace}/${r.name}`}>
              <td>{r.level}</td>
              <td>
                <Link
                  to={resourcePath({
                    cluster,
                    namespace: r.namespace,
                    group: 'core',
                    kind: 'ServiceAccount',
                    name: r.name,
                  })}
                >
                  {r.name}
                </Link>
              </td>
              <td>{r.namespace}</td>
              <td>{r.reasons.join('; ')}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

/** The ServiceAccounts of the cluster and the RBAC objects the user may read. Needs no CRDs. */
export function ServiceAccountPermissions({ cluster }: { cluster: string }) {
  const rbac = useRbacData(cluster);
  const accounts = useLive({ cluster, group: '', version: 'v1', resource: 'serviceaccounts', namespace: '' });
  if (rbac.loading || accounts.status === 'loading') return <p role="status">Loading…</p>;
  return (
    <ServiceAccountPermissionsView
      cluster={cluster}
      accounts={accounts.items}
      data={rbac.data}
      forbidden={rbac.forbidden}
      truncated={rbac.truncated}
    />
  );
}
