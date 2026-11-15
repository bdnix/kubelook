export interface Rule {
  apiGroups?: string[];
  resources?: string[];
  verbs?: string[];
  resourceNames?: string[];
}

/** A Role or a ClusterRole; an aggregated ClusterRole already carries the rules the cluster put together. */
export interface RoleLike {
  metadata: { name: string; namespace?: string };
  rules?: Rule[];
  aggregationRule?: unknown;
}

export interface Subject {
  kind: string;
  name: string;
  namespace?: string;
  apiGroup?: string;
}

/** A RoleBinding or a ClusterRoleBinding. */
export interface BindingLike {
  metadata: { name: string; namespace?: string };
  roleRef: { kind: string; name: string; apiGroup?: string };
  subjects?: Subject[];
}

export interface RbacData {
  roles: RoleLike[];
  clusterRoles: RoleLike[];
  roleBindings: BindingLike[];
  clusterRoleBindings: BindingLike[];
}

export interface Query {
  verb: string;
  /** The API group; `core` and the empty string are the core group. */
  group: string;
  /** The resource, with its subresource if any: `pods`, `pods/exec`. */
  resource: string;
  /** Only this namespace; empty or absent means every namespace. */
  namespace?: string;
}

export interface Ref {
  kind: string;
  name: string;
  namespace?: string;
}

export interface Grant {
  /** `all` for a cluster-wide binding, otherwise the namespace of the RoleBinding. */
  scope: string;
  binding: Ref;
  role: Ref;
  /** Set when every matching rule is limited to these object names. */
  onlyNamed: string[] | undefined;
}

export interface WhoCanSubject {
  kind: string;
  name: string;
  namespace?: string;
  grants: Grant[];
}

export interface Dangling {
  binding: Ref;
  role: Ref;
}

export interface WhoCanResult {
  subjects: WhoCanSubject[];
  dangling: Dangling[];
}

const has = (list: string[] | undefined, want: string, wildcard: (item: string) => boolean = () => false) =>
  (list ?? []).some((v) => v === '*' || v === want || wildcard(v));

/** Whether a rule covers the query. `pods/*` covers every subresource of pods. */
function covers(rule: Rule, q: Query): boolean {
  const group = q.group === 'core' ? '' : q.group;
  return (
    has(rule.verbs, q.verb) &&
    has(rule.apiGroups, group) &&
    has(rule.resources, q.resource, (r) => r.endsWith('/*') && q.resource.startsWith(r.slice(0, -1)))
  );
}

/**
 * What a role grants for the query: null when nothing; `undefined` when it grants it on all objects; or the
 * names it is limited to.
 */
function grantOf(role: RoleLike, q: Query): string[] | undefined | null {
  const matching = (role.rules ?? []).filter((r) => covers(r, q));
  if (matching.length === 0) return null;
  if (matching.some((r) => (r.resourceNames ?? []).length === 0)) return undefined;
  return [...new Set(matching.flatMap((r) => r.resourceNames ?? []))].sort();
}

const ref = (kind: string, b: { name: string; namespace?: string }): Ref =>
  b.namespace ? { kind, name: b.name, namespace: b.namespace } : { kind, name: b.name };

/**
 * Who can do `verb` on `resource`, from the roles and bindings the user could list. This reflects RBAC objects
 * only; other authorizers are not seen.
 */
export function whoCan(q: Query, data: RbacData): WhoCanResult {
  const clusterRoles = new Map(data.clusterRoles.map((r) => [r.metadata.name, r]));
  const roles = new Map(data.roles.map((r) => [`${r.metadata.namespace ?? ''}/${r.metadata.name}`, r]));
  const bySubject = new Map<string, WhoCanSubject>();
  const dangling: Dangling[] = [];

  const visit = (b: BindingLike, bindingKind: string) => {
    const namespaced = bindingKind === 'RoleBinding';
    const ns = b.metadata.namespace ?? '';
    const roleKind = b.roleRef.kind;
    const role =
      roleKind === 'Role' && namespaced
        ? roles.get(`${ns}/${b.roleRef.name}`)
        : roleKind === 'ClusterRole'
          ? clusterRoles.get(b.roleRef.name)
          : undefined;
    if (!role) {
      dangling.push({
        binding: ref(bindingKind, b.metadata),
        role: { kind: roleKind, name: b.roleRef.name },
      });
      return;
    }
    if (namespaced && q.namespace && q.namespace !== ns) return;
    const only = grantOf(role, q);
    if (only === null) return;
    const grant: Grant = {
      scope: namespaced ? ns : 'all',
      binding: ref(bindingKind, b.metadata),
      role:
        roleKind === 'Role'
          ? ref('Role', { name: b.roleRef.name, namespace: ns })
          : ref('ClusterRole', { name: b.roleRef.name }),
      onlyNamed: only,
    };
    for (const s of b.subjects ?? []) {
      const key = `${s.kind}|${s.namespace ?? ''}|${s.name}`;
      let entry = bySubject.get(key);
      if (!entry) {
        entry = {
          kind: s.kind,
          name: s.name,
          ...(s.namespace ? { namespace: s.namespace } : {}),
          grants: [],
        };
        bySubject.set(key, entry);
      }
      entry.grants.push(grant);
    }
  };

  data.clusterRoleBindings.forEach((b) => visit(b, 'ClusterRoleBinding'));
  data.roleBindings.forEach((b) => visit(b, 'RoleBinding'));

  const subjects = [...bySubject.values()].sort(
    (a, b) =>
      a.kind.localeCompare(b.kind) ||
      (a.namespace ?? '').localeCompare(b.namespace ?? '') ||
      a.name.localeCompare(b.name),
  );
  return { subjects, dangling };
}
