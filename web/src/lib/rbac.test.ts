import { describe, expect, it } from 'vitest';
import { whoCan, type RbacData } from './rbac';

const meta = (name: string, namespace?: string) => ({ name, ...(namespace ? { namespace } : {}) });
const rule = (r: { verbs: string[]; resources: string[]; apiGroups?: string[]; resourceNames?: string[] }) => ({
  apiGroups: [''],
  ...r,
});
const user = (name: string) => ({ kind: 'User', name });
const group = (name: string) => ({ kind: 'Group', name });
const sa = (name: string, namespace: string) => ({ kind: 'ServiceAccount', name, namespace });

const empty: RbacData = { roles: [], clusterRoles: [], roleBindings: [], clusterRoleBindings: [] };
const q = { verb: 'delete', group: '', resource: 'pods' };

const names = (r: ReturnType<typeof whoCan>) => r.subjects.map((s) => `${s.kind}:${s.namespace ?? ''}/${s.name}`);

describe('whoCan: cluster-wide', () => {
  const data: RbacData = {
    ...empty,
    clusterRoles: [{ metadata: meta('pod-admin'), rules: [rule({ verbs: ['delete', 'get'], resources: ['pods'] })] }],
    clusterRoleBindings: [{ metadata: meta('crb'), roleRef: { kind: 'ClusterRole', name: 'pod-admin' }, subjects: [user('ada'), group('ops')] }],
  };

  it('a ClusterRoleBinding to a ClusterRole with a matching rule lists the subjects for all namespaces', () => {
    const r = whoCan(q, data);
    expect(names(r)).toEqual(['Group:/ops', 'User:/ada']);
    expect(r.subjects[1]?.grants).toEqual([
      { scope: 'all', binding: { kind: 'ClusterRoleBinding', name: 'crb' }, role: { kind: 'ClusterRole', name: 'pod-admin' }, onlyNamed: undefined },
    ]);
    expect(r.dangling).toEqual([]);
  });

  it('applies in every namespace when one is asked about', () => {
    expect(names(whoCan({ ...q, namespace: 'prod' }, data))).toEqual(['Group:/ops', 'User:/ada']);
  });

  it('is not listed for a verb, resource or group the rule does not cover', () => {
    expect(whoCan({ ...q, verb: 'create' }, data).subjects).toEqual([]);
    expect(whoCan({ ...q, resource: 'secrets' }, data).subjects).toEqual([]);
    expect(whoCan({ ...q, group: 'apps', resource: 'pods' }, data).subjects).toEqual([]);
  });

  it('a subresource is a different resource', () => {
    expect(whoCan({ ...q, resource: 'pods/exec' }, data).subjects).toEqual([]);
    const withExec: RbacData = {
      ...data,
      clusterRoles: [{ metadata: meta('exec'), rules: [rule({ verbs: ['create'], resources: ['pods/exec'] })] }],
      clusterRoleBindings: [{ metadata: meta('b'), roleRef: { kind: 'ClusterRole', name: 'exec' }, subjects: [user('eve')] }],
    };
    expect(names(whoCan({ verb: 'create', group: '', resource: 'pods/exec' }, withExec))).toEqual(['User:/eve']);
    expect(whoCan({ verb: 'create', group: '', resource: 'pods' }, withExec).subjects).toEqual([]);
  });
});

describe('whoCan: namespaced bindings', () => {
  const data: RbacData = {
    ...empty,
    roles: [{ metadata: meta('editor', 'dev'), rules: [rule({ verbs: ['delete'], resources: ['pods'] })] }],
    clusterRoles: [{ metadata: meta('cr'), rules: [rule({ verbs: ['delete'], resources: ['pods'] })] }],
    roleBindings: [
      { metadata: meta('rb-role', 'dev'), roleRef: { kind: 'Role', name: 'editor' }, subjects: [sa('builder', 'dev')] },
      { metadata: meta('rb-cr', 'qa'), roleRef: { kind: 'ClusterRole', name: 'cr' }, subjects: [sa('tester', 'qa')] },
    ],
  };

  it('a RoleBinding to a Role or to a ClusterRole lists its subjects for that namespace only', () => {
    const r = whoCan(q, data);
    expect(names(r)).toEqual(['ServiceAccount:dev/builder', 'ServiceAccount:qa/tester']);
    expect(r.subjects.map((s) => s.grants.map((g) => g.scope))).toEqual([['dev'], ['qa']]);
  });

  it('asking about one namespace leaves out the bindings of the others', () => {
    expect(names(whoCan({ ...q, namespace: 'dev' }, data))).toEqual(['ServiceAccount:dev/builder']);
    expect(whoCan({ ...q, namespace: 'other' }, data).subjects).toEqual([]);
  });

  it('a Role of another namespace is not the role of the binding', () => {
    const wrong: RbacData = {
      ...data,
      roleBindings: [{ metadata: meta('rb', 'qa'), roleRef: { kind: 'Role', name: 'editor' }, subjects: [user('x')] }],
    };
    const r = whoCan(q, wrong);
    expect(r.subjects).toEqual([]);
    expect(r.dangling).toEqual([{ binding: { kind: 'RoleBinding', name: 'rb', namespace: 'qa' }, role: { kind: 'Role', name: 'editor' } }]);
  });
});

describe('whoCan: wildcards, named objects, aggregation', () => {
  it('* matches every verb, resource and group', () => {
    const data: RbacData = {
      ...empty,
      clusterRoles: [{ metadata: meta('root'), rules: [{ apiGroups: ['*'], resources: ['*'], verbs: ['*'] }] }],
      clusterRoleBindings: [{ metadata: meta('b'), roleRef: { kind: 'ClusterRole', name: 'root' }, subjects: [user('admin')] }],
    };
    expect(names(whoCan({ verb: 'patch', group: 'apps', resource: 'deployments' }, data))).toEqual(['User:/admin']);
    expect(names(whoCan(q, data))).toEqual(['User:/admin']);
  });

  it('a rule limited to named objects lists the subject with that note', () => {
    const data: RbacData = {
      ...empty,
      clusterRoles: [{ metadata: meta('one'), rules: [rule({ verbs: ['delete'], resources: ['pods'], resourceNames: ['web-1', 'web-2'] })] }],
      clusterRoleBindings: [{ metadata: meta('b'), roleRef: { kind: 'ClusterRole', name: 'one' }, subjects: [user('ada')] }],
    };
    const grant = whoCan(q, data).subjects[0]?.grants[0];
    expect(grant?.onlyNamed).toEqual(['web-1', 'web-2']);
  });

  it('an unrestricted rule beats a restricted one in the same role', () => {
    const data: RbacData = {
      ...empty,
      clusterRoles: [
        {
          metadata: meta('both'),
          rules: [rule({ verbs: ['delete'], resources: ['pods'], resourceNames: ['web-1'] }), rule({ verbs: ['*'], resources: ['pods'] })],
        },
      ],
      clusterRoleBindings: [{ metadata: meta('b'), roleRef: { kind: 'ClusterRole', name: 'both' }, subjects: [user('ada')] }],
    };
    expect(whoCan(q, data).subjects[0]?.grants[0]?.onlyNamed).toBeUndefined();
  });

  it('an aggregated ClusterRole is read through its aggregated rules', () => {
    const data: RbacData = {
      ...empty,
      clusterRoles: [
        {
          metadata: meta('admin'),
          aggregationRule: { clusterRoleSelectors: [{ matchLabels: { x: 'y' } }] },
          rules: [rule({ verbs: ['delete'], resources: ['pods'] })],
        },
      ],
      clusterRoleBindings: [{ metadata: meta('b'), roleRef: { kind: 'ClusterRole', name: 'admin' }, subjects: [user('ada')] }],
    };
    expect(names(whoCan(q, data))).toEqual(['User:/ada']);
  });

  it('the core group may be written core or empty', () => {
    const data: RbacData = {
      ...empty,
      clusterRoles: [{ metadata: meta('r'), rules: [rule({ verbs: ['delete'], resources: ['pods'] })] }],
      clusterRoleBindings: [{ metadata: meta('b'), roleRef: { kind: 'ClusterRole', name: 'r' }, subjects: [user('ada')] }],
    };
    expect(names(whoCan({ ...q, group: 'core' }, data))).toEqual(['User:/ada']);
  });
});

describe('whoCan: subjects and dangling bindings', () => {
  it('lists a subject once, with every grant, sorted by kind, namespace and name', () => {
    const data: RbacData = {
      ...empty,
      clusterRoles: [{ metadata: meta('r'), rules: [rule({ verbs: ['delete'], resources: ['pods'] })] }],
      clusterRoleBindings: [
        { metadata: meta('b1'), roleRef: { kind: 'ClusterRole', name: 'r' }, subjects: [user('zed'), sa('s', 'b'), user('ada')] },
        { metadata: meta('b2'), roleRef: { kind: 'ClusterRole', name: 'r' }, subjects: [user('ada'), sa('s', 'a'), group('g')] },
      ],
      roleBindings: [{ metadata: meta('rb', 'dev'), roleRef: { kind: 'ClusterRole', name: 'r' }, subjects: [user('ada')] }],
    };
    const r = whoCan(q, data);
    expect(names(r)).toEqual(['Group:/g', 'ServiceAccount:a/s', 'ServiceAccount:b/s', 'User:/ada', 'User:/zed']);
    const ada = r.subjects.find((s) => s.name === 'ada');
    expect(ada?.grants.map((g) => `${g.binding.name}:${g.scope}`).sort()).toEqual(['b1:all', 'b2:all', 'rb:dev']);
  });

  it('a binding whose role does not exist is dangling, with the same binding listed once', () => {
    const data: RbacData = {
      ...empty,
      clusterRoleBindings: [{ metadata: meta('gone'), roleRef: { kind: 'ClusterRole', name: 'missing' }, subjects: [user('ada')] }],
      roleBindings: [{ metadata: meta('rb', 'dev'), roleRef: { kind: 'Role', name: 'nothing' }, subjects: [] }],
    };
    const r = whoCan(q, data);
    expect(r.subjects).toEqual([]);
    expect(r.dangling).toEqual([
      { binding: { kind: 'ClusterRoleBinding', name: 'gone' }, role: { kind: 'ClusterRole', name: 'missing' } },
      { binding: { kind: 'RoleBinding', name: 'rb', namespace: 'dev' }, role: { kind: 'Role', name: 'nothing' } },
    ]);
  });

  it('copes with roles without rules and bindings without subjects', () => {
    const data: RbacData = {
      ...empty,
      clusterRoles: [{ metadata: meta('empty') }],
      clusterRoleBindings: [{ metadata: meta('b'), roleRef: { kind: 'ClusterRole', name: 'empty' } }],
    };
    expect(whoCan(q, data)).toEqual({ subjects: [], dangling: [] });
  });
});
