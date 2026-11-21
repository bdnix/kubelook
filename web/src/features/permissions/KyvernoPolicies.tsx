import { conditionStatus } from '../../kinds/columns';
import { getArray, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';

/** Failed results of PolicyReports and ClusterPolicyReports, counted by policy. */
export function kyvernoViolations(reports: K8sObject[]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const r of reports) {
    for (const res of getArray(r, 'results')) {
      if (getString(res, 'result') === 'fail')
        out[getString(res, 'policy')] = (out[getString(res, 'policy')] ?? 0) + 1;
    }
  }
  return out;
}

export interface PolicyRef {
  kind: string;
  object: K8sObject;
}

/** The Kyverno policies with their readiness, their action and how many results fail. Read only. */
export function KyvernoPolicies({ policies, reports }: { policies: PolicyRef[]; reports: K8sObject[] }) {
  const violations = kyvernoViolations(reports);
  return (
    <section aria-label="Kyverno">
      <h2>Kyverno</h2>
      <p>
        The violations come from the policy reports (wgpolicyk8s.io); this source is unconfirmed until the
        spike finding is recorded.
      </p>
      <table aria-label="Kyverno policies">
        <thead>
          <tr>
            <th>Policy</th>
            <th>Kind</th>
            <th>Ready</th>
            <th>Action</th>
            <th>Violations</th>
          </tr>
        </thead>
        <tbody>
          {[...policies]
            .sort(
              (a, b) =>
                (violations[getString(b.object, 'metadata.name')] ?? 0) -
                (violations[getString(a.object, 'metadata.name')] ?? 0),
            )
            .map((p) => {
              const name = getString(p.object, 'metadata.name');
              return (
                <tr key={`${p.kind}/${getString(p.object, 'metadata.namespace')}/${name}`}>
                  <td>{name}</td>
                  <td>{p.kind}</td>
                  <td>{conditionStatus('Ready')(p.object)}</td>
                  <td>{getString(p.object, 'spec.validationFailureAction')}</td>
                  <td>{violations[name] ?? 0}</td>
                </tr>
              );
            })}
        </tbody>
      </table>
    </section>
  );
}
