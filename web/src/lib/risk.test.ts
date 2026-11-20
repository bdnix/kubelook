import { describe, expect, it } from 'vitest';
import type { RbacData, Rule } from './rbac';
import { effectiveRules, riskLevel, type EffectiveRule } from './risk';

const at = (rule: Rule, scope = 'all'): EffectiveRule => ({ rule, scope });
const level = (rule: Rule, scope = 'all') => riskLevel([at(rule, scope)]).level;
const rule = (verbs: string[], resources: string[], apiGroups = ['']): Rule => ({ verbs, resources, apiGroups });

describe('riskLevel', () => {
  it.each([
    ['everything', rule(['*'], ['*'], ['*']), 'all', 'critical'],
    ['escalate', rule(['escalate'], ['clusterroles'], ['rbac.authorization.k8s.io']), 'ns', 'critical'],
    ['bind', rule(['bind'], ['roles'], ['rbac.authorization.k8s.io']), 'ns', 'critical'],
    ['impersonate', rule(['impersonate'], ['users']), 'ns', 'critical'],
    ['read secrets everywhere', rule(['get'], ['secrets']), 'all', 'critical'],
    ['list secrets everywhere', rule(['list', 'watch'], ['secrets']), 'all', 'critical'],
    ['exec cluster-wide', rule(['create'], ['pods/exec']), 'all', 'critical'],
    ['attach cluster-wide', rule(['create'], ['pods/attach']), 'all', 'critical'],
    ['nodes/proxy', rule(['get'], ['nodes/proxy']), 'ns', 'critical'],
    ['write clusterroles', rule(['create'], ['clusterroles'], ['rbac.authorization.k8s.io']), 'all', 'critical'],
    ['write clusterrolebindings', rule(['update'], ['clusterrolebindings'], ['rbac.authorization.k8s.io']), 'all', 'critical'],
    ['read secrets in a namespace', rule(['get'], ['secrets']), 'shop', 'high'],
    ['write workloads everywhere', rule(['create'], ['deployments'], ['apps']), 'all', 'high'],
    ['delete pods everywhere', rule(['delete'], ['pods']), 'all', 'high'],
    ['write cronjobs everywhere', rule(['patch'], ['cronjobs'], ['batch']), 'all', 'high'],
    ['write roles', rule(['create'], ['roles'], ['rbac.authorization.k8s.io']), 'shop', 'high'],
    ['write rolebindings', rule(['create'], ['rolebindings'], ['rbac.authorization.k8s.io']), 'shop', 'high'],
    ['exec in a namespace', rule(['create'], ['pods/exec']), 'shop', 'high'],
    ['create tokens', rule(['create'], ['serviceaccounts/token']), 'shop', 'high'],
    ['write workloads in a namespace', rule(['update'], ['statefulsets'], ['apps']), 'shop', 'medium'],
    ['write configmaps in a namespace', rule(['create'], ['configmaps']), 'shop', 'medium'],
    ['write services in a namespace', rule(['delete'], ['services']), 'shop', 'medium'],
    ['write ingresses in a namespace', rule(['patch'], ['ingresses'], ['networking.k8s.io']), 'shop', 'medium'],
    ['read pods', rule(['get', 'list', 'watch'], ['pods']), 'all', 'low'],
    ['read configmaps everywhere', rule(['get'], ['configmaps']), 'all', 'low'],
    ['nothing', rule([], []), 'all', 'low'],
  ] as const)('%s → %s', (_name, r, scope, want) => {
    expect(level(r, scope)).toBe(want);
  });

  it('understands wildcards in resources and API groups', () => {
    expect(level(rule(['get'], ['*'], ['*']), 'all')).toBe('critical');
    expect(level(rule(['create'], ['*'], ['apps']), 'shop')).toBe('medium');
    expect(level(rule(['*'], ['secrets'], ['*']), 'shop')).toBe('high');
    expect(level(rule(['create'], ['deployments'], ['batch']), 'all')).toBe('low');
  });

  it('takes the highest level and names the rules that caused it', () => {
    const r = riskLevel([at(rule(['get'], ['pods'])), at(rule(['get'], ['secrets']), 'shop'), at(rule(['create'], ['configmaps']), 'shop')]);
    expect(r.level).toBe('high');
    expect(r.reasons).toEqual(['get secrets in shop']);
  });

  it('is low without rules', () => {
    expect(riskLevel([])).toEqual({ level: 'low', reasons: [] });
  });
});

describe('effectiveRules', () => {
  const data: RbacData = {
    roles: [{ metadata: { name: 'editor', namespace: 'shop' }, rules: [rule(['create'], ['configmaps'])] }],
    clusterRoles: [
      { metadata: { name: 'reader' }, rules: [rule(['get'], ['pods'])] },
      { metadata: { name: 'admin' }, rules: [rule(['*'], ['*'], ['*'])] },
    ],
    roleBindings: [
      { metadata: { name: 'b1', namespace: 'shop' }, roleRef: { kind: 'Role', name: 'editor' }, subjects: [{ kind: 'ServiceAccount', name: 'app', namespace: 'shop' }] },
      { metadata: { name: 'b2', namespace: 'shop' }, roleRef: { kind: 'ClusterRole', name: 'reader' }, subjects: [{ kind: 'ServiceAccount', name: 'app', namespace: 'shop' }] },
      { metadata: { name: 'b3', namespace: 'shop' }, roleRef: { kind: 'Role', name: 'missing' }, subjects: [{ kind: 'ServiceAccount', name: 'app', namespace: 'shop' }] },
      { metadata: { name: 'b4', namespace: 'other' }, roleRef: { kind: 'ClusterRole', name: 'admin' }, subjects: [{ kind: 'ServiceAccount', name: 'app', namespace: 'shop' }] },
    ],
    clusterRoleBindings: [
      { metadata: { name: 'c1' }, roleRef: { kind: 'ClusterRole', name: 'reader' }, subjects: [{ kind: 'Group', name: 'system:serviceaccounts' }] },
      { metadata: { name: 'c2' }, roleRef: { kind: 'ClusterRole', name: 'reader' }, subjects: [{ kind: 'Group', name: 'system:serviceaccounts:shop' }] },
      { metadata: { name: 'c3' }, roleRef: { kind: 'ClusterRole', name: 'admin' }, subjects: [{ kind: 'ServiceAccount', name: 'someone-else', namespace: 'shop' }] },
      { metadata: { name: 'c4' }, roleRef: { kind: 'ClusterRole', name: 'admin' }, subjects: [{ kind: 'Group', name: 'system:serviceaccounts:other' }] },
      { metadata: { name: 'c5' }, roleRef: { kind: 'ClusterRole', name: 'admin' }, subjects: [{ kind: 'User', name: 'app' }] },
    ],
  };

  it('resolves the bindings of the account: its own, the namespace group and the all-accounts group', () => {
    const r = effectiveRules({ name: 'app', namespace: 'shop' }, data);
    expect(r.map((x) => `${x.scope}:${x.rule.resources?.join(',')}`)).toEqual([
      'all:pods', // c1: system:serviceaccounts
      'all:pods', // c2: system:serviceaccounts:shop
      'shop:configmaps', // b1: the Role
      'shop:pods', // b2: a ClusterRole bound in the namespace
      'shop:*', // b4 is in another namespace but names this account
    ]);
  });

  it('is empty for an account nothing binds', () => {
    expect(effectiveRules({ name: 'unused', namespace: 'empty' }, { ...data, clusterRoleBindings: [], roleBindings: [] })).toEqual([]);
  });
});
