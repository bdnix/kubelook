import { useMemo, useRef, useState, type KeyboardEvent, type PointerEvent, type WheelEvent } from 'react';
import { forceLayout, type Point } from '../lib/forceLayout';

export type GraphStatus = 'ok' | 'warning' | 'error' | 'unknown';

export interface GraphNode {
  id: string;
  kind: string;
  name: string;
  status: GraphStatus;
  /** Lines shown while the node is hovered or focused. */
  facts?: string[];
}

export interface GraphEdge {
  from: string;
  to: string;
}

const STATUS_TEXT: Record<GraphStatus, string> = {
  ok: 'ready',
  warning: 'in progress',
  error: 'failed',
  unknown: 'unknown',
};
const MIN_ZOOM = 0.2;
const MAX_ZOOM = 4;
const ZOOM_STEP = 1.1;
const DRAG_THRESHOLD = 4;
const WIDTH = 800;
const HEIGHT = 500;
const NODE_W = 150;
const NODE_H = 36;

export interface GraphProps {
  label: string;
  nodes: GraphNode[];
  edges: GraphEdge[];
  onOpen: (node: GraphNode) => void;
}

interface Drag {
  kind: 'pan' | 'node';
  id?: string;
  startX: number;
  startY: number;
  moved: boolean;
}

/**
 * A graph drawn as SVG with a deterministic layout. The wheel zooms, dragging the background pans, dragging a node
 * moves it. Nodes are buttons: click, Enter or Space opens them; hovering or focusing one shows its facts.
 */
export function Graph({ label, nodes, edges, onOpen }: GraphProps) {
  const layout = useMemo(
    () =>
      forceLayout(
        nodes.map((n) => ({ id: n.id })),
        edges.map((e) => ({ source: e.from, target: e.to })),
      ),
    [nodes, edges],
  );
  const [moved, setMoved] = useState<Record<string, Point>>({});
  const [view, setView] = useState({ x: 0, y: 0, k: 1 });
  const [hover, setHover] = useState<string>();
  const drag = useRef<Drag | undefined>(undefined);
  const justDragged = useRef(false);
  const at = (id: string): Point => moved[id] ?? layout.get(id) ?? { x: 0, y: 0 };

  if (nodes.length === 0) return <p>Nothing to show.</p>;

  const onWheel = (e: WheelEvent) => {
    const factor = e.deltaY < 0 ? ZOOM_STEP : 1 / ZOOM_STEP;
    setView((v) => ({ ...v, k: Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, Number((v.k * factor).toFixed(4)))) }));
  };
  const onPointerDown = (e: PointerEvent, id?: string) => {
    e.stopPropagation();
    drag.current = { kind: id ? 'node' : 'pan', id, startX: e.clientX, startY: e.clientY, moved: false };
  };
  const onPointerMove = (e: PointerEvent) => {
    const d = drag.current;
    if (!d) return;
    const dx = e.clientX - d.startX;
    const dy = e.clientY - d.startY;
    if (!d.moved && Math.hypot(dx, dy) < DRAG_THRESHOLD) return;
    d.moved = true;
    d.startX = e.clientX;
    d.startY = e.clientY;
    if (d.kind === 'pan') setView((v) => ({ ...v, x: v.x + dx, y: v.y + dy }));
    else if (d.id) {
      const id = d.id;
      setMoved((m) => {
        const p = m[id] ?? layout.get(id) ?? { x: 0, y: 0 };
        return { ...m, [id]: { x: p.x + dx / view.k, y: p.y + dy / view.k } };
      });
    }
  };
  const onPointerUp = () => {
    justDragged.current = drag.current?.moved ?? false;
    drag.current = undefined;
  };
  const open = (n: GraphNode) => {
    if (justDragged.current) {
      justDragged.current = false;
      return;
    }
    onOpen(n);
  };
  const onKey = (e: KeyboardEvent, n: GraphNode) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onOpen(n);
    }
  };
  const hovered = nodes.find((n) => n.id === hover);

  return (
    <div className="graph">
      <svg
        role="img"
        aria-label={label}
        viewBox={`${-WIDTH / 2} ${-HEIGHT / 2} ${WIDTH} ${HEIGHT}`}
        width="100%"
        height={HEIGHT}
        onWheel={onWheel}
        onPointerDown={(e) => onPointerDown(e)}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
      >
        <g data-testid="graph-content" transform={`translate(${view.x} ${view.y}) scale(${view.k})`}>
          {edges.map((e) => {
            const a = at(e.from);
            const b = at(e.to);
            return (
              <line
                key={`${e.from}>${e.to}`}
                x1={a.x}
                y1={a.y}
                x2={b.x}
                y2={b.y}
                stroke="currentColor"
                strokeOpacity={0.4}
              />
            );
          })}
          {nodes.map((n) => {
            const p = at(n.id);
            return (
              <g
                key={n.id}
                role="button"
                tabIndex={0}
                aria-label={`${n.kind} ${n.name}, ${STATUS_TEXT[n.status]}`}
                transform={`translate(${p.x} ${p.y})`}
                className={`graph-node graph-${n.status}`}
                onPointerDown={(e) => onPointerDown(e, n.id)}
                onClick={() => open(n)}
                onKeyDown={(e) => onKey(e, n)}
                onMouseEnter={() => setHover(n.id)}
                onMouseLeave={() => setHover(undefined)}
                onFocus={() => setHover(n.id)}
                onBlur={() => setHover(undefined)}
              >
                <rect x={-NODE_W / 2} y={-NODE_H / 2} width={NODE_W} height={NODE_H} rx={6} />
                <text textAnchor="middle" y={-2} fontSize={10}>
                  {n.kind}
                </text>
                <text textAnchor="middle" y={11} fontSize={11}>
                  {n.name}
                </text>
              </g>
            );
          })}
        </g>
      </svg>
      {hovered?.facts && hovered.facts.length > 0 && (
        <div role="tooltip" className="graph-facts">
          <strong>{`${hovered.kind} ${hovered.name}`}</strong>
          <ul>
            {hovered.facts.map((f, i) => (
              <li key={i}>{f}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
