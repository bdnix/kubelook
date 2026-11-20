import type { GraphNode, GraphStatus } from '../../components/Graph';

/** A node as the `map.open` channel sends it. */
export interface MapNode {
  id: string;
  group: string;
  kind: string;
  namespace: string;
  name: string;
  /** The phase of a pod; other kinds have none. */
  status?: string;
}

export interface NodeStyle {
  color: string;
  icon: string;
  statusText: string;
  graphStatus: GraphStatus;
}

interface KindStyle {
  color: string;
  icon: string;
}

/** One colour and one icon per kind. */
const KINDS: Record<string, KindStyle> = {
  Pod: { color: '#4f8f5b', icon: 'P' },
  Deployment: { color: '#3b6fb6', icon: 'D' },
  StatefulSet: { color: '#6b5bb5', icon: 'S' },
  DaemonSet: { color: '#2f8f9d', icon: 'A' },
  Service: { color: '#c77d2a', icon: 'V' },
  Ingress: { color: '#b5517a', icon: 'I' },
  HTTPRoute: { color: '#8a4fb5', icon: 'R' },
  GRPCRoute: { color: '#b58a3a', icon: 'G' },
  ConfigMap: { color: '#7a8c3a', icon: 'C' },
  Secret: { color: '#a04f4f', icon: 'K' },
  PersistentVolumeClaim: { color: '#4a7f7f', icon: 'Q' },
  HelmRelease: { color: '#5a6b8c', icon: 'H' },
};

const FALLBACK: KindStyle = { color: '#777777', icon: '?' };

interface PhaseStyle {
  color: string;
  text: string;
  status: GraphStatus;
  icon?: string;
}

/** Pods take the colour of their phase; a failed pod also gets an icon and the word "failed". */
const PHASES: Record<string, PhaseStyle> = {
  Running: { color: '#3f9b55', text: 'running', status: 'ok' },
  Succeeded: { color: '#5b7fb5', text: 'succeeded', status: 'ok' },
  Pending: { color: '#d19a2a', text: 'pending', status: 'warning' },
  Failed: { color: '#c0392b', text: 'failed', status: 'error', icon: '⚠' },
  Unknown: { color: '#7d7d7d', text: 'unknown', status: 'unknown' },
};

export function nodeStyle(n: MapNode): NodeStyle {
  const kind = KINDS[n.kind] ?? FALLBACK;
  if (n.kind === 'Pod') {
    const phase = PHASES[n.status ?? ''] ?? PHASES.Unknown;
    const p = phase as PhaseStyle;
    return { color: p.color, icon: p.icon ?? kind.icon, statusText: p.text, graphStatus: p.status };
  }
  return { color: kind.color, icon: kind.icon, statusText: '', graphStatus: 'unknown' };
}

/** The nodes of the server as graph nodes. */
export function mapNodes(nodes: MapNode[]): GraphNode[] {
  return nodes.map((n) => {
    const s = nodeStyle(n);
    const facts = [
      ...(n.namespace ? [`Namespace ${n.namespace}`] : []),
      ...(n.kind === 'Pod' && n.status ? [`Phase ${n.status}`] : []),
    ];
    return {
      id: n.id,
      kind: n.kind,
      name: n.name,
      status: s.graphStatus,
      statusText: s.statusText,
      color: s.color,
      icon: s.icon,
      facts,
    };
  });
}

/** What the colours and icons mean. */
export function Legend() {
  return (
    <section aria-label="Legend">
      <h2>Legend</h2>
      <ul>
        {Object.entries(KINDS).map(([kind, s]) => (
          <li key={kind}>
            <span aria-hidden="true" style={{ color: s.color }}>{`${s.icon} `}</span>
            {kind}
          </li>
        ))}
      </ul>
      <ul aria-label="Pod phases">
        {Object.entries(PHASES)
          .filter(([phase]) => phase !== 'Unknown')
          .map(([phase, s]) => (
            <li key={phase}>
              <span aria-hidden="true" style={{ color: s.color }}>{`${s.icon ?? 'P'} `}</span>
              {`Pod ${phase}`}
            </li>
          ))}
      </ul>
      <p>Failed pods are marked with a warning icon and the word “failed”.</p>
    </section>
  );
}
