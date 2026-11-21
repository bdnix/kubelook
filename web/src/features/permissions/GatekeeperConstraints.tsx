import { getArray, getNumber, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';

export interface ConstraintRef {
  kind: string;
  object: K8sObject;
}

export interface ConstraintSummary {
  action: string;
  total: number;
  listed: number;
  cut: boolean;
}

/** Gatekeeper keeps a limited number of violations per constraint: `total` can exceed what is listed. */
export function gatekeeperSummary(o: K8sObject): ConstraintSummary {
  const listed = getArray(o, 'status.violations').length;
  const total = getNumber(o, 'status.totalViolations', listed);
  return { action: getString(o, 'spec.enforcementAction') || 'deny', total, listed, cut: total > listed };
}

/** The Gatekeeper constraints with their action and violations. Read only. */
export function GatekeeperConstraints({ constraints }: { constraints: ConstraintRef[] }) {
  return (
    <section aria-label="Gatekeeper">
      <h2>Gatekeeper</h2>
      {constraints.map((c) => {
        const s = gatekeeperSummary(c.object);
        const name = getString(c.object, 'metadata.name');
        return (
          <section key={`${c.kind}/${name}`} aria-label={`Constraint ${name}`}>
            <h3>{name}</h3>
            <p>{`${c.kind}, action ${s.action}, ${s.total} violations`}</p>
            {s.cut && (
              <p>{`Showing ${s.listed} of ${s.total} violations (Gatekeeper keeps a limited number per constraint).`}</p>
            )}
            <ul>
              {getArray(c.object, 'status.violations').map((v, i) => (
                <li
                  key={i}
                >{`${getString(v, 'kind')} ${getString(v, 'namespace') ? `${getString(v, 'namespace')}/` : ''}${getString(v, 'name')}: ${getString(v, 'message')}`}</li>
              ))}
            </ul>
          </section>
        );
      })}
    </section>
  );
}
