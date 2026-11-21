import { forceCenter, forceLink, forceManyBody, forceSimulation, type SimulationNodeDatum } from 'd3-force';

export interface LayoutNode {
  id: string;
}

export interface LayoutLink {
  source: string;
  target: string;
}

export interface Point {
  x: number;
  y: number;
}

/** The number of ticks of the simulation: fixed, so the result does not depend on how long it ran. */
export const TICKS = 300;

/** A small seeded generator (mulberry32): d3-force uses its random source only to untangle nodes on the same spot. */
function seeded(seed: number): () => number {
  let a = seed;
  return () => {
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/**
 * Places nodes with a force simulation run for a fixed number of ticks, synchronously and with a seeded random
 * source, so the same graph always gets the same layout. Links to nodes that are not there are ignored.
 */
export function forceLayout(nodes: LayoutNode[], links: LayoutLink[]): Map<string, Point> {
  const data = nodes.map((n): SimulationNodeDatum & { id: string } => ({ id: n.id }));
  const ids = new Set(nodes.map((n) => n.id));
  const usable = links.filter((l) => ids.has(l.source) && ids.has(l.target)).map((l) => ({ ...l }));
  const sim = forceSimulation(data)
    .randomSource(seeded(1))
    .force(
      'link',
      forceLink<SimulationNodeDatum & { id: string }, { source: string; target: string }>(usable)
        .id((d) => d.id)
        .distance(90),
    )
    .force('charge', forceManyBody().strength(-260))
    .force('center', forceCenter(0, 0))
    .stop();
  sim.tick(TICKS);
  return new Map(data.map((d) => [d.id, { x: d.x ?? 0, y: d.y ?? 0 }]));
}
