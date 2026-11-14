import { describe, expect, it } from 'vitest';
import { expectToleratesMissingFields, meta, row } from '../testing';
import {
  clusterRoleBindingsKind,
  clusterRolesKind,
  roleBindingsKind,
  rolesKind,
  serviceAccountsKind,
} from './access';

describe('access control kinds', () => {
  it('ServiceAccount counts secrets', () => {
    expect(row(serviceAccountsKind, { metadata: meta('sa', 'ns'), secrets: [{}, {}] }).secrets).toBe(2);
    expectToleratesMissingFields(serviceAccountsKind);
  });

  it('Role and ClusterRole count rules', () => {
    expect(row(rolesKind, { metadata: meta('r', 'ns'), rules: [{}] }).rules).toBe(1);
    expect(row(clusterRolesKind, { metadata: meta('r'), rules: [{}, {}, {}] }).rules).toBe(3);
    expectToleratesMissingFields(rolesKind);
    expectToleratesMissingFields(clusterRolesKind);
  });

  it('bindings show the role and the subjects', () => {
    const b = {
      metadata: meta('b', 'ns'),
      roleRef: { kind: 'ClusterRole', name: 'admin' },
      subjects: [
        { kind: 'User', name: 'alice' },
        { kind: 'ServiceAccount', name: 'ci' },
      ],
    };
    expect(row(roleBindingsKind, b)).toMatchObject({
      role: 'ClusterRole/admin',
      subjects: 'User/alice,ServiceAccount/ci',
    });
    expect(row(clusterRoleBindingsKind, b)).toMatchObject({ role: 'ClusterRole/admin' });
    expect(row(roleBindingsKind, { metadata: meta('b', 'ns') })).toMatchObject({ role: '', subjects: '' });
    expectToleratesMissingFields(roleBindingsKind);
    expectToleratesMissingFields(clusterRoleBindingsKind);
  });
});
