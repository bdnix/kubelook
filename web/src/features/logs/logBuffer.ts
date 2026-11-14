export interface LogLine {
  ts: string;
  text: string;
  /** A note inserted by the viewer (dropped lines), not a line of the container. */
  marker?: boolean;
}

/** Appends lines and keeps at most `max`, dropping the oldest. */
export function appendLines(buffer: LogLine[], lines: LogLine[], max: number): LogLine[] {
  const all = buffer.concat(lines);
  return all.length > max ? all.slice(all.length - max) : all;
}

/** Appends a visible "… N lines dropped …" line. */
export function markDropped(buffer: LogLine[], count: number, max: number): LogLine[] {
  const note = `… ${count} ${count === 1 ? 'line' : 'lines'} dropped …`;
  return appendLines(buffer, [{ ts: '', text: note, marker: true }], max);
}
