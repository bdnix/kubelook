import { describe, expect, it } from 'vitest';
import { treemap } from './treemap';

const area = (r: { w: number; h: number }) => r.w * r.h;

describe('treemap', () => {
  const items = [
    { id: 'a', value: 6 },
    { id: 'b', value: 6 },
    { id: 'c', value: 4 },
    { id: 'd', value: 3 },
    { id: 'e', value: 2 },
    { id: 'f', value: 2 },
    { id: 'g', value: 1 },
  ];

  it('gives each item an area proportional to its value, filling the box', () => {
    const rects = treemap(items, 600, 400);
    expect(rects).toHaveLength(7);
    const total = items.reduce((s, i) => s + i.value, 0);
    for (const r of rects) {
      const item = items.find((i) => i.id === r.id);
      expect(area(r)).toBeCloseTo(((item?.value ?? 0) / total) * 600 * 400, 3);
    }
    expect(rects.reduce((s, r) => s + area(r), 0)).toBeCloseTo(600 * 400, 3);
  });

  it('keeps every rectangle inside the box and without overlap', () => {
    const rects = treemap(items, 600, 400);
    for (const r of rects) {
      expect(r.x).toBeGreaterThanOrEqual(-1e-9);
      expect(r.y).toBeGreaterThanOrEqual(-1e-9);
      expect(r.x + r.w).toBeLessThanOrEqual(600 + 1e-9);
      expect(r.y + r.h).toBeLessThanOrEqual(400 + 1e-9);
    }
    for (let i = 0; i < rects.length; i++) {
      for (let j = i + 1; j < rects.length; j++) {
        const a = rects[i];
        const b = rects[j];
        if (!a || !b) continue;
        const overlapW = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x);
        const overlapH = Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y);
        expect(overlapW <= 1e-6 || overlapH <= 1e-6).toBe(true);
      }
    }
  });

  it('is deterministic and does not depend on the input order of equal values', () => {
    expect(treemap(items, 600, 400)).toEqual(treemap(items, 600, 400));
    expect(treemap([...items].reverse(), 600, 400).map((r) => r.id).sort()).toEqual(items.map((i) => i.id).sort());
  });

  it('handles nothing, one item, zero values and a box with no size', () => {
    expect(treemap([], 100, 100)).toEqual([]);
    expect(treemap([{ id: 'x', value: 5 }], 100, 50)).toEqual([{ id: 'x', x: 0, y: 0, w: 100, h: 50 }]);
    const zero = treemap([{ id: 'a', value: 0 }, { id: 'b', value: 0 }], 100, 100);
    expect(zero.reduce((s, r) => s + area(r), 0)).toBeCloseTo(10000, 3);
    expect(zero.map((r) => r.id).sort()).toEqual(['a', 'b']);
    expect(treemap(items, 0, 100)).toEqual([]);
  });
});
