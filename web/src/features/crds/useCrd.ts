import { useQuery } from '@tanstack/react-query';
import { useApi } from '../../api/context';
import { crdName, type CrdLike } from '../../kinds/generic';

/**
 * The CustomResourceDefinition behind a custom kind, or `undefined` while it loads, when it is not needed and
 * when the user may not read CRDs (the list then shows Name, Namespace and Age only).
 */
export function useCrd(cluster: string, group: string, resource: string, enabled = true): CrdLike | undefined {
  const api = useApi();
  const query = useQuery({
    queryKey: ['crd', cluster, group, resource],
    queryFn: () =>
      api.get<CrdLike>(
        `/clusters/${encodeURIComponent(cluster)}/resources/apiextensions.k8s.io/v1/customresourcedefinitions/${encodeURIComponent(crdName(group, resource))}`,
      ),
    enabled,
    staleTime: 60_000,
    retry: false,
  });
  return query.data;
}
