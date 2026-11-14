export interface WindowInput {
  rowCount: number;
  rowHeight: number;
  viewportHeight: number;
  scrollTop: number;
  overscan: number;
}

/** The half-open range `[start, end)` of rows to render for the scroll position. */
export function visibleRange({
  rowCount,
  rowHeight,
  viewportHeight,
  scrollTop,
  overscan,
}: WindowInput): [number, number] {
  if (rowCount <= 0 || rowHeight <= 0) return [0, 0];
  const first = Math.floor(Math.max(0, scrollTop) / rowHeight);
  const last = Math.ceil((Math.max(0, scrollTop) + Math.max(0, viewportHeight)) / rowHeight);
  const start = Math.min(Math.max(0, first - overscan), rowCount);
  const end = Math.min(rowCount, last + overscan);
  return [start, Math.max(start, end)];
}
