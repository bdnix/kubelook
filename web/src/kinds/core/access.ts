import { getArray, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { countColumn, defineKind } from '../columns';
import type { Column, KindDescriptor } from '../types';

const roleRef: Column = {
  id: 'role',
  title: 'Role',
  value: (o) => {
    const name = getString(o, 'roleRef.name');
    return name ? `${getString(o, 'roleRef.kind')}/${name}` : '';
  },
};

function subjects(o: K8sObject): string {
  return getArray(o, 'subjects')
    .map((s) => `${getString(s, 'kind')}/${getString(s, 'name')}`)
    .join(',');
}

const subjectsColumn: Column = { id: 'subjects', title: 'Subjects', value: subjects };

export const serviceAccountsKind: KindDescriptor = defineKind({
  group: '',
  kind: 'ServiceAccount',
  resource: 'serviceaccounts',
  namespaced: true,
  title: 'ServiceAccounts',
  section: 'Access Control',
  actions: ['delete'],
  columns: [countColumn('secrets', 'Secrets', 'secrets')],
});

export const rolesKind: KindDescriptor = defineKind({
  group: 'rbac.authorization.k8s.io',
  kind: 'Role',
  resource: 'roles',
  namespaced: true,
  title: 'Roles',
  section: 'Access Control',
  actions: ['delete'],
  columns: [countColumn('rules', 'Rules', 'rules')],
});

export const clusterRolesKind: KindDescriptor = defineKind({
  group: 'rbac.authorization.k8s.io',
  kind: 'ClusterRole',
  resource: 'clusterroles',
  namespaced: false,
  title: 'ClusterRoles',
  section: 'Access Control',
  actions: ['delete'],
  columns: [countColumn('rules', 'Rules', 'rules')],
});

export const roleBindingsKind: KindDescriptor = defineKind({
  group: 'rbac.authorization.k8s.io',
  kind: 'RoleBinding',
  resource: 'rolebindings',
  namespaced: true,
  title: 'RoleBindings',
  section: 'Access Control',
  actions: ['delete'],
  columns: [roleRef, subjectsColumn],
});

export const clusterRoleBindingsKind: KindDescriptor = defineKind({
  group: 'rbac.authorization.k8s.io',
  kind: 'ClusterRoleBinding',
  resource: 'clusterrolebindings',
  namespaced: false,
  title: 'ClusterRoleBindings',
  section: 'Access Control',
  actions: ['delete'],
  columns: [roleRef, subjectsColumn],
});
