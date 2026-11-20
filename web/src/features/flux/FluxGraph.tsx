import { useQueries } from '@tanstack/react-query';
import { useMemo } from 'react';
import { useNavigate } from 'react-router';
import { useApi } from '../../api/context';
import { Graph } from '../../components/Graph';
import { findFluxKind } from '../../kinds/flux';
import { getString } from '../../lib/path';
import { resourcePath } from '../../lib/paths';
import type { K8sObject } from '../../state/live';
import { buildFluxGraph, type InventoryEntry } from './graph';
import { nodeId } from './graph';

/** The inventories are read for at most this many Kustomizations and HelmReleases. */
const MAX_INVENTORIES = 30;

export const inventoryUrl = (
  cluster: string,
  group: string,
  kind: string,
  namespace: string,
  name: string,
) => {
  const q = new URLSearchParams({ group, kind, namespace, name });
  return `/clusters/${encodeURIComponent(cluster)}/flux/inventory?${q.toString()}`;
};

/** The source → Kustomization or HelmRelease → workload graph of the objects in the lists. */
export function FluxGraph({ cluster, objects }: { cluster: string; objects: K8sObject[] }) {
  const api = useApi();
  const navigate = useNavigate();
  const managers = useMemo(
    () =>
      objects
        .filter((o) => ['Kustomization', 'HelmRelease'].includes(getString(o, 'kind')))
        .slice(0, MAX_INVENTORIES),
    [objects],
  );
  const results = useQueries({
    queries: managers.map((o) => {
      const group = getString(o, 'apiVersion').split('/')[0] ?? '';
      const kind = getString(o, 'kind');
      const namespace = getString(o, 'metadata.namespace');
      const name = getString(o, 'metadata.name');
      return {
        queryKey: ['flux-inventory', cluster, group, kind, namespace, name],
        queryFn: () =>
          api.get<{ entries: InventoryEntry[] }>(inventoryUrl(cluster, group, kind, namespace, name)),
        staleTime: 30_000,
        retry: false,
      };
    }),
  });
  const inventories = useMemo(() => {
    const m = new Map<string, InventoryEntry[]>();
    managers.forEach((o, i) => {
      const entries = results[i]?.data?.entries;
      if (entries)
        m.set(
          nodeId(
            getString(o, 'apiVersion').split('/')[0] ?? '',
            getString(o, 'kind'),
            getString(o, 'metadata.namespace'),
            getString(o, 'metadata.name'),
          ),
          entries,
        );
    });
    return m;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- the results array is new on every render; its data decides
  }, [managers, results.map((r) => r.dataUpdatedAt).join(',')]);
  const graph = useMemo(() => buildFluxGraph(objects, inventories), [objects, inventories]);
  const byId = useMemo(
    () =>
      new Map(
        objects.map((o) => [
          nodeId(
            getString(o, 'apiVersion').split('/')[0] ?? '',
            getString(o, 'kind'),
            getString(o, 'metadata.namespace'),
            getString(o, 'metadata.name'),
          ),
          o,
        ]),
      ),
    [objects],
  );
  return (
    <section aria-label="Flux graph">
      <h2>Graph</h2>
      <Graph
        label="Sources, Kustomizations, HelmReleases and workloads"
        nodes={graph.nodes}
        edges={graph.edges}
        onOpen={(n) => {
          const o = byId.get(n.id);
          const [group = '', kind = '', namespace = '', name = ''] = n.id.split('/');
          const target =
            o && findFluxKind(group, kind)
              ? { group, kind, namespace, name }
              : { group: group || 'core', kind, namespace, name };
          void navigate(resourcePath({ cluster, ...target }));
        }}
      />
    </section>
  );
}
