import type { Discovery } from '../../api/discovery';
import { FLUX_KINDS, isSuspended, type FluxKindDef } from '../../kinds/flux';
import { conditionStatus } from '../../kinds/columns';
import { getArray, getNumber, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';

export interface Summary {
  total: number;
  /** False for kinds whose objects report no conditions (an Alert or a Provider on `v1beta3`): only totals count. */
  reportsStatus: boolean;
  ready: number;
  failed: number;
  inProgress: number;
  suspended: number;
}

/**
 * Counts the objects of a kind. Each object is counted once: a suspended object only as suspended; otherwise
 * `Reconciling=True` or `Ready=Unknown` is in progress, `Ready=False` failed and `Ready=True` ready.
 */
export function summarize(kind: FluxKindDef, objects: K8sObject[]): Summary {
  const s: Summary = {
    total: objects.length,
    reportsStatus: objects.some((o) => getArray(o, 'status.conditions').length > 0),
    ready: 0,
    failed: 0,
    inProgress: 0,
    suspended: 0,
  };
  for (const o of objects) {
    if (isSuspended(kind, o)) s.suspended++;
    else if (!s.reportsStatus) continue;
    else if (conditionStatus('Reconciling')(o) === 'True') s.inProgress++;
    else {
      const ready = conditionStatus('Ready')(o);
      if (ready === 'True') s.ready++;
      else if (ready === 'False') s.failed++;
      else s.inProgress++;
    }
  }
  return s;
}

export interface ServedFluxKind {
  def: FluxKindDef;
  group: string;
  version: string;
  resource: string;
}

/** The Flux kinds the cluster serves, in the version it prefers; the dashboard hides the others. */
export function servedFluxKinds(discovery: Discovery): ServedFluxKind[] {
  return FLUX_KINDS.flatMap((def) => {
    const r = discovery.resources.find((x) => x.preferred && x.group === def.group && x.kind === def.kind);
    return r ? [{ def, group: r.group, version: r.version, resource: r.resource }] : [];
  });
}

export interface Controller {
  name: string;
  version: string;
  ready: boolean;
  desired: number;
  available: number;
}

const PART_OF = 'app.kubernetes.io/part-of';
const VERSION = 'app.kubernetes.io/version';

/** The Flux controllers among the Deployments, by the `app.kubernetes.io/part-of=flux` label, sorted by name. */
export function controllerHealth(deployments: K8sObject[]): Controller[] {
  return deployments
    .filter(
      (d) => (d.metadata as { labels?: Record<string, string> } | undefined)?.labels?.[PART_OF] === 'flux',
    )
    .map((d) => {
      const desired = getNumber(d, 'spec.replicas', 1);
      const available = getNumber(d, 'status.availableReplicas');
      const labels = (d.metadata as { labels?: Record<string, string> }).labels ?? {};
      return {
        name: getString(d, 'metadata.name'),
        version: labels[VERSION] ?? '',
        ready: available >= desired,
        desired,
        available,
      };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}
