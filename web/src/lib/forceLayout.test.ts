import { describe, expect, it } from 'vitest';
import { forceLayout } from './forceLayout';

const nodes = ['a', 'b', 'c', 'd'].map((id) => ({ id }));
const links = [
  { source: 'a', target: 'b' },
  { source: 'b', target: 'c' },
  { source: 'a', target: 'd' },
];

describe('forceLayout', () => {
  it('gives the same coordinates for the same input, every time', () => {
    const first = [...forceLayout(nodes, links).entries()];
    const second = [...forceLayout(nodes, links).entries()];
    expect(second).toEqual(first);
  });

  it('places every node at finite, distinct coordinates', () => {
    const out = forceLayout(nodes, links);
    expect([...out.keys()].sort()).toEqual(['a', 'b', 'c', 'd']);
    const spots = new Set<string>();
    for (const { x, y } of out.values()) {
      expect(Number.isFinite(x)).toBe(true);
      expect(Number.isFinite(y)).toBe(true);
      spots.add(`${x.toFixed(2)},${y.toFixed(2)}`);
    }
    expect(spots.size).toBe(4);
  });

  it('keeps linked nodes closer than a node that is far down the chain', () => {
    const chain = Array.from({ length: 6 }, (_, i) => ({ id: `n${i}` }));
    const out = forceLayout(chain, chain.slice(1).map((n, i) => ({ source: `n${i}`, target: n.id })));
    const d = (a: string, b: string) => Math.hypot((out.get(a)?.x ?? 0) - (out.get(b)?.x ?? 0), (out.get(a)?.y ?? 0) - (out.get(b)?.y ?? 0));
    expect(d('n0', 'n1')).toBeLessThan(d('n0', 'n5'));
  });

  it('ignores a link to a node that is not there', () => {
    const out = forceLayout(nodes, [...links, { source: 'a', target: 'missing' }]);
    expect(out.size).toBe(4);
  });

  it('handles no nodes and one node', () => {
    expect(forceLayout([], []).size).toBe(0);
    expect(forceLayout([{ id: 'only' }], []).get('only')).toBeDefined();
  });
});
