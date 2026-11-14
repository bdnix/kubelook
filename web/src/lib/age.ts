const MINUTE = 60;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;
const YEAR = 365 * DAY;

/** A kubectl-style age: `45s`, `12m`, `3h`, `2d`, `5w`, `2y`. `now` is injected so tests are deterministic. */
export function formatAge(timestamp: string | undefined, nowMs: number): string {
  if (!timestamp) return '';
  const then = Date.parse(timestamp);
  if (Number.isNaN(then)) return '';
  const seconds = Math.max(0, Math.floor((nowMs - then) / 1000));
  if (seconds < MINUTE) return `${seconds}s`;
  if (seconds < HOUR) return `${Math.floor(seconds / MINUTE)}m`;
  if (seconds < DAY) return `${Math.floor(seconds / HOUR)}h`;
  if (seconds < WEEK) return `${Math.floor(seconds / DAY)}d`;
  if (seconds < YEAR) return `${Math.floor(seconds / WEEK)}w`;
  return `${Math.floor(seconds / YEAR)}y`;
}
