import type { BindingLike, RbacData, Rule, Subject } from './rbac';

export type Risk = 'critical' | 'high' | 'medium' | 'low';

/** A rule an account holds, and where: `all` for a cluster-wide grant, otherwise a namespace. */
export interface EffectiveRule {
  rule: Rule;
  scope: string;
}

const ORDER: Risk[] = ['low', 'medium', 'high', 'critical'];

/** Resources of the table of risk rules (A18) and the API group each lives in. */
const GROUP: Record<string, string> = {
  pods: '',
  secrets: '',
  nodes: '',
  serviceaccounts: '',
  configmaps: '',
  services: '',
  deployments: 'apps',
  statefulsets: 'apps',
  daemonsets: 'apps',
  jobs: 'batch',
  cronjobs: 'batch',
  ingresses: 'networking.k8s.io',
  roles: 'rbac.authorization.k8s.io',
  rolebindings: 'rbac.authorization.k8s.io',
  clusterroles: 'rbac.authorization.k8s.io',
  clusterrolebindings: 'rbac.authorization.k8s.io',
};

const WRITE = ['create', 'update', 'patch', 'delete', 'deletecollection'];
const READ = ['get', 'list', 'watch'];
const WORKLOADS = ['pods', 'deployments', 'statefulsets', 'daemonsets', 'jobs', 'cronjobs'];
const RBAC = ['roles', 'rolebindings', 'clusterroles', 'clusterrolebindings'];

const hasVerb = (r: Rule, ...verbs: string[]) => (r.verbs ?? []).some((v) => v === '*' || verbs.includes(v));

/** Whether the rule covers the resource (a subresource is written `pods/exec`) in its API group. */
function covers(r: Rule, resource: string): boolean {
  const base = resource.split('/')[0] ?? resource;
  const group = GROUP[base] ?? '';
  const groups = r.apiGroups ?? [''];
  if (!groups.includes('*') && !groups.includes(group)) return false;
  return (r.resources ?? []).some(
    (x) => x === '*' || x === resource || (x.endsWith('/*') && resource.startsWith(x.slice(0, -1))),
  );
}

const coversAny = (r: Rule, resources: string[]) => resources.some((x) => covers(r, x));

interface Hit {
  level: Risk;
  reason: string;
}

function ruleHit(r: Rule, scope: string): Hit {
  const everywhere = scope === 'all';
  const reason = (what: string) => ({
    reason: `${(r.verbs ?? []).join(', ')} ${(r.resources ?? []).join(', ')}${what}`,
  });
  const where = ` in ${everywhere ? 'all namespaces' : scope}`;
  const verbs = r.verbs ?? [];
  const resources = r.resources ?? [];
  const critical = (): boolean =>
    (verbs.includes('*') && resources.includes('*')) ||
    verbs.some((v) => ['escalate', 'bind', 'impersonate'].includes(v)) ||
    (verbs.includes('*') && coversAny(r, RBAC) && !resources.includes('*')) ||
    (everywhere && hasVerb(r, ...READ) && covers(r, 'secrets')) ||
    (everywhere && hasVerb(r, 'create') && coversAny(r, ['pods/exec', 'pods/attach'])) ||
    covers(r, 'nodes/proxy') ||
    (hasVerb(r, ...WRITE) && coversAny(r, ['clusterroles', 'clusterrolebindings']));
  if (critical()) return { level: 'critical', ...reason(where) };
  const high =
    (hasVerb(r, ...READ) && covers(r, 'secrets')) ||
    (everywhere && hasVerb(r, ...WRITE) && coversAny(r, WORKLOADS)) ||
    (hasVerb(r, ...WRITE) && coversAny(r, ['roles', 'rolebindings'])) ||
    (hasVerb(r, 'create') && covers(r, 'pods/exec')) ||
    (hasVerb(r, 'create') && covers(r, 'serviceaccounts/token'));
  if (high) return { level: 'high', ...reason(where) };
  if (hasVerb(r, ...WRITE) && coversAny(r, [...WORKLOADS, 'configmaps', 'services', 'ingresses']))
    return { level: 'medium', ...reason(where) };
  return { level: 'low', reason: '' };
}

/**
 * The risk of the rules an account holds: the highest level any rule reaches, with the rules that reach it.
 * It looks at RBAC rules only; it is an aid, not a verdict.
 */
export function riskLevel(rules: EffectiveRule[]): { level: Risk; reasons: string[] } {
  let level: Risk = 'low';
  let reasons: string[] = [];
  for (const { rule, scope } of rules) {
    const hit = ruleHit(rule, scope);
    if (ORDER.indexOf(hit.level) > ORDER.indexOf(level)) {
      level = hit.level;
      reasons = [hit.reason];
    } else if (hit.level === level && hit.reason && !reasons.includes(hit.reason)) {
      reasons.push(hit.reason);
    }
  }
  return { level, reasons };
}

function binds(subjects: Subject[] | undefined, sa: { name: string; namespace: string }): boolean {
  return (subjects ?? []).some(
    (s) =>
      (s.kind === 'ServiceAccount' && s.name === sa.name && (s.namespace ?? '') === sa.namespace) ||
      (s.kind === 'Group' &&
        (s.name === 'system:serviceaccounts' || s.name === `system:serviceaccounts:${sa.namespace}`)),
  );
}

/** The rules a ServiceAccount holds through RoleBindings and ClusterRoleBindings, cluster-wide grants first. */
export function effectiveRules(sa: { name: string; namespace: string }, data: RbacData): EffectiveRule[] {
  const out: EffectiveRule[] = [];
  const rulesOf = (b: BindingLike, namespace: string | undefined): Rule[] | undefined => {
    if (b.roleRef.kind === 'ClusterRole')
      return data.clusterRoles.find((r) => r.metadata.name === b.roleRef.name)?.rules;
    return data.roles.find((r) => r.metadata.name === b.roleRef.name && r.metadata.namespace === namespace)
      ?.rules;
  };
  for (const b of data.clusterRoleBindings.filter((x) => binds(x.subjects, sa))) {
    for (const rule of rulesOf(b, undefined) ?? []) out.push({ rule, scope: 'all' });
  }
  for (const b of data.roleBindings.filter((x) => binds(x.subjects, sa))) {
    for (const rule of rulesOf(b, b.metadata.namespace) ?? [])
      out.push({ rule, scope: b.metadata.namespace ?? '' });
  }
  return out;
}
