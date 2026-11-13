const enc = encodeURIComponent;

/** The URL of a cluster. IDs such as `k:file/ctx` contain `:` and `/`, so every segment is percent-encoded. */
export function clusterPath(id: string): string {
  return `/c/${enc(id)}`;
}

export interface ResourceRef {
  cluster: string;
  /** Absent or empty for cluster-scoped kinds. */
  namespace?: string;
  /** The API group; the empty (core) group is written `core`. */
  group: string;
  kind: string;
  name: string;
}

/** The deep link `/c/<cluster>/ns/<ns>/<group>/<kind>/<name>` (no `ns` part when cluster-scoped). */
export function resourcePath({ cluster, namespace, group, kind, name }: ResourceRef): string {
  const ns = namespace ? `/ns/${enc(namespace)}` : '';
  return `${clusterPath(cluster)}${ns}/${enc(group || 'core')}/${enc(kind)}/${enc(name)}`;
}
