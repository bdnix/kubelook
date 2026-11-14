import { useQuery } from '@tanstack/react-query';
import { useApi } from '../../api/context';
import { findSchemaRef } from '../../lib/openapi';

export interface KindSchema {
  /** The whole OpenAPI v3 document of the group and version. */
  schema: unknown;
  /** The `$ref` of the kind inside it, when the document has one. */
  schemaRef: string | undefined;
}

/** The OpenAPI schema of a kind, fetched once per group and version (`GET /clusters/{cluster}/openapi/{group}/{version}`). */
export function useSchema(
  cluster: string,
  group: string,
  version: string,
  kind: string,
  enabled = true,
): KindSchema | undefined {
  const api = useApi();
  const g = group === '' ? 'core' : group;
  const query = useQuery({
    queryKey: ['openapi', cluster, g, version],
    queryFn: () =>
      api.get<unknown>(
        `/clusters/${encodeURIComponent(cluster)}/openapi/${encodeURIComponent(g)}/${encodeURIComponent(version)}`,
      ),
    enabled,
    staleTime: 5 * 60_000,
    retry: false,
  });
  if (query.data === undefined) return undefined;
  return { schema: query.data, schemaRef: findSchemaRef(query.data, g, version, kind) };
}
