export interface TreemapItem {
  id: string;
  value: number;
}

export interface Rect {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

/** The worst aspect ratio of a row of areas laid along a side of the given length. */
function worst(row: number[], side: number): number {
  const sum = row.reduce((a, b) => a + b, 0);
  const max = Math.max(...row);
  const min = Math.min(...row);
  return Math.max((side * side * max) / (sum * sum), (sum * sum) / (side * side * min));
}

/**
 * A squarified treemap (Bruls, Huijing, van Wijk): rectangles whose areas are proportional to the values, as
 * square as the layout allows. The same items always give the same rectangles. Items with no value share the
 * box equally.
 */
export function treemap(items: TreemapItem[], width: number, height: number): Rect[] {
  if (items.length === 0 || width <= 0 || height <= 0) return [];
  const clean = items.map((i) => ({ id: i.id, v: Math.max(0, i.value) }));
  const total = clean.reduce((s, i) => s + i.v, 0);
  const values = total === 0 ? clean.map((i) => ({ ...i, v: 1 })) : clean;
  const sum = values.reduce((s, i) => s + i.v, 0);
  const sorted = [...values].sort((a, b) => b.v - a.v || a.id.localeCompare(b.id));
  const scale = (width * height) / sum;
  const areas = sorted.map((i) => i.v * scale);
  const out: Rect[] = [];
  let box = { x: 0, y: 0, w: width, h: height };
  let start = 0;
  while (start < sorted.length) {
    const side = Math.min(box.w, box.h);
    let end = start + 1;
    while (
      end < sorted.length &&
      worst(areas.slice(start, end + 1), side) <= worst(areas.slice(start, end), side)
    )
      end++;
    const row = areas.slice(start, end);
    const rowSum = row.reduce((a, b) => a + b, 0);
    if (box.w >= box.h) {
      const w = rowSum / box.h;
      let y = box.y;
      row.forEach((a, i) => {
        const h = a / w;
        out.push({ id: (sorted[start + i] as { id: string }).id, x: box.x, y, w, h });
        y += h;
      });
      box = { x: box.x + w, y: box.y, w: box.w - w, h: box.h };
    } else {
      const h = rowSum / box.w;
      let x = box.x;
      row.forEach((a, i) => {
        const w = a / h;
        out.push({ id: (sorted[start + i] as { id: string }).id, x, y: box.y, w, h });
        x += w;
      });
      box = { x: box.x, y: box.y + h, w: box.w, h: box.h - h };
    }
    start = end;
  }
  return out;
}
