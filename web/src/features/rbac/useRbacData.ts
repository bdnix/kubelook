import { useQueries } from '@tanstack/react-query';
import { ApiError } from '../../api/client';
import { useApi } from '../../api/context';
import type { BindingLike, RbacData, RoleLike } from '../../lib/rbac';

const KINDS = [
  { key: 'roles', resource: 'roles', title: 'Roles' },
  { key: 'clusterRoles', resource: 'clusterroles', title: 'ClusterRoles' },
  { key: 'roleBindings', resource: 'rolebindings', title: 'RoleBindings' },
  { key: 'clusterRoleBindings', resource: 'clusterrolebindings', title: 'ClusterRoleBindings' },
] as const;

/** The most objects read of one kind; more than this makes the answer incomplete. */
export const RBAC_LIMIT = 5000;

interface Answer {
  items: unknown[];
  continue?: string;
}

export interface RbacState {
  loading: boolean;
  data: RbacData;
  /** The kinds the user may not list. */
  forbidden: string[];
  /** The kinds that were cut off at the limit. */
  truncated: string[];
  /** The kinds that failed for another reason. */
  failed: string[];
}

/** Reads the four RBAC kinds of a cluster with the user's own credentials. */
export function useRbacData(cluster: string): RbacState {
  const api = useApi();
  const results = useQueries({
    queries: KINDS.map((k) => ({
      queryKey: ['rbac', cluster, k.resource],
      queryFn: () =>
        api.get<Answer>(
          `/clusters/${encodeURIComponent(cluster)}/resources/rbac.authorization.k8s.io/v1/${k.resource}?limit=${RBAC_LIMIT}`,
        ),
      retry: false,
      staleTime: 30_000,
    })),
  });
  const state: RbacState = {
    loading: results.some((r) => r.isPending),
    data: { roles: [], clusterRoles: [], roleBindings: [], clusterRoleBindings: [] },
    forbidden: [],
    truncated: [],
    failed: [],
  };
  results.forEach((r, i) => {
    const kind = KINDS[i];
    if (!kind) return;
    if (r.isError) {
      (r.error instanceof ApiError && r.error.code === 'forbidden' ? state.forbidden : state.failed).push(
        kind.title,
      );
      return;
    }
    if (!r.data) return;
    const items = r.data.items as Array<RoleLike & BindingLike>;
    if (kind.key === 'roles' || kind.key === 'clusterRoles') state.data[kind.key] = items;
    else state.data[kind.key] = items;
    if (r.data.continue) state.truncated.push(kind.title);
  });
  return state;
}
