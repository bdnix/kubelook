import type { GraphEdge, GraphNode, GraphStatus } from '../../components/Graph';
import { FLUX_KINDS, findFluxKind, fluxStatus, isSuspended, revisionOf } from '../../kinds/flux';
import { getArray, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';

/** An object a Kustomization or a HelmRelease manages (`GET …/flux/inventory`). */
export interface InventoryEntry {
  group: string;
  version: string;
  kind: string;
  namespace: string;
  name: string;
}

/** The inventories by node id of the Kustomization or HelmRelease. */
export type Inventories = Map<string, InventoryEntry[]>;

const WORKLOADS = ['Deployment', 'StatefulSet', 'DaemonSet'];

export const nodeId = (group: string, kind: string, namespace: string, name: string) =>
  `${group}/${kind}/${namespace}/${name}`;

const groupOfApiVersion = (apiVersion: string) =>
  apiVersion.includes('/') ? (apiVersion.split('/')[0] ?? '') : '';
const groupOfKind = (kind: string, fallback = '') =>
  FLUX_KINDS.find((k) => k.kind === kind)?.group ?? fallback;

const SEVERITY: Record<string, GraphStatus> = {
  ok: 'ok',
  warning: 'warning',
  error: 'error',
  unknown: 'unknown',
};

function facts(o: K8sObject, suspended: boolean): string[] {
  const out: string[] = [];
  const ready = getArray(o, 'status.conditions').find((c) => getString(c, 'type') === 'Ready');
  if (ready)
    out.push(
      `Ready=${getString(ready, 'status')}${getString(ready, 'message') ? `: ${getString(ready, 'message')}` : ''}`,
    );
  const revision = revisionOf(o);
  if (revision) out.push(`Revision ${revision}`);
  if (suspended) out.push('Suspended');
  return out;
}

interface Ref {
  kind: string;
  name: string;
  namespace?: string;
  apiVersion?: string;
}

function refsOf(o: K8sObject): { refs: Ref[]; depends: Ref[] } {
  const spec = (o.spec ?? {}) as Record<string, unknown>;
  const refs: Ref[] = [];
  const add = (v: unknown) => {
    const r = v as Ref | undefined;
    if (r && typeof r.kind === 'string' && typeof r.name === 'string') refs.push(r);
  };
  add(spec.sourceRef);
  add((spec.chart as { spec?: { sourceRef?: unknown } } | undefined)?.spec?.sourceRef);
  add(spec.chartRef);
  const depends = (Array.isArray(spec.dependsOn) ? spec.dependsOn : [])
    .filter(
      (d): d is { name: string; namespace?: string } => typeof (d as { name?: unknown })?.name === 'string',
    )
    .map((d) => ({
      kind: getString(o, 'kind'),
      name: d.name,
      namespace: d.namespace,
      apiVersion: getString(o, 'apiVersion'),
    }));
  return { refs, depends };
}

/**
 * The graph of sources → Kustomizations and HelmReleases → workloads. A source named by a reference that the
 * lists do not hold gets a placeholder node; a dependency that is not there gets no edge.
 */
export function buildFluxGraph(
  objects: K8sObject[],
  inventories: Inventories = new Map(),
): { nodes: GraphNode[]; edges: GraphEdge[] } {
  const nodes = new Map<string, GraphNode>();
  const edges = new Map<string, GraphEdge>();
  const link = (from: string, to: string) => edges.set(`${from}>${to}`, { from, to });

  for (const o of objects) {
    const kind = getString(o, 'kind');
    const group = groupOfApiVersion(getString(o, 'apiVersion'));
    const def = findFluxKind(group, kind);
    if (!def) continue;
    const suspended = isSuspended(def, o);
    const id = nodeId(group, kind, getString(o, 'metadata.namespace'), getString(o, 'metadata.name'));
    nodes.set(id, {
      id,
      kind,
      name: getString(o, 'metadata.name'),
      status: suspended ? 'warning' : (SEVERITY[fluxStatus(o).severity] ?? 'unknown'),
      facts: facts(o, suspended),
    });
  }

  for (const o of objects) {
    const kind = getString(o, 'kind');
    const group = groupOfApiVersion(getString(o, 'apiVersion'));
    if (!findFluxKind(group, kind)) continue;
    const namespace = getString(o, 'metadata.namespace');
    const to = nodeId(group, kind, namespace, getString(o, 'metadata.name'));
    const { refs, depends } = refsOf(o);
    for (const r of refs) {
      const from = nodeId(
        groupOfKind(r.kind, groupOfApiVersion(r.apiVersion ?? '')),
        r.kind,
        r.namespace ?? namespace,
        r.name,
      );
      if (!nodes.has(from))
        nodes.set(from, {
          id: from,
          kind: r.kind,
          name: r.name,
          status: 'unknown',
          facts: ['Not in the lists shown'],
        });
      link(from, to);
    }
    for (const d of depends) {
      const from = nodeId(group, kind, d.namespace ?? namespace, d.name);
      if (nodes.has(from)) link(from, to);
    }
    for (const e of inventories.get(to) ?? []) {
      if (!WORKLOADS.includes(e.kind)) continue;
      const wid = nodeId(e.group, e.kind, e.namespace, e.name);
      if (!nodes.has(wid))
        nodes.set(wid, {
          id: wid,
          kind: e.kind,
          name: e.name,
          status: 'unknown',
          facts: [`Namespace ${e.namespace}`],
        });
      link(to, wid);
    }
  }
  return { nodes: [...nodes.values()], edges: [...edges.values()] };
}
