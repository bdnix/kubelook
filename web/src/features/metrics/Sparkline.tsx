export interface SparklineProps {
  label: string;
  /** [time, value] pairs, oldest first. */
  points: ReadonlyArray<readonly [number, number]>;
  width?: number;
  height?: number;
}

/** A small line of a series. Without at least two points there is nothing to draw. */
export function Sparkline({ label, points, width = 120, height = 24 }: SparklineProps) {
  if (points.length < 2) {
    return (
      <span role="img" aria-label={label}>
        No data
      </span>
    );
  }
  const xs = points.map((p) => p[0]);
  const ys = points.map((p) => p[1]);
  const [x0, x1] = [Math.min(...xs), Math.max(...xs)];
  const [y0, y1] = [Math.min(...ys), Math.max(...ys)];
  const at = ([x, y]: readonly [number, number]) => {
    const px = x1 === x0 ? 0 : ((x - x0) / (x1 - x0)) * width;
    const py = y1 === y0 ? height / 2 : height - ((y - y0) / (y1 - y0)) * height;
    return `${Math.round(px * 100) / 100},${Math.round(py * 100) / 100}`;
  };
  return (
    <svg role="img" aria-label={label} width={width} height={height} viewBox={`0 0 ${width} ${height}`}>
      <polyline fill="none" stroke="currentColor" strokeWidth="1.5" points={points.map(at).join(' ')} />
    </svg>
  );
}
