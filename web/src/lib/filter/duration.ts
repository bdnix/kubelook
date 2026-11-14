const UNITS: Record<string, number> = { s: 1000, m: 60_000, h: 3_600_000, d: 86_400_000, w: 604_800_000 };

const PATTERN = /^(\d+)([smhdw])$/;

/** True for `30s`, `15m`, `2h`, `7d` and `1w`. */
export function isDuration(text: string): boolean {
  return PATTERN.test(text);
}

/** A duration such as `7d` in milliseconds. */
export function parseDuration(text: string): number {
  const m = PATTERN.exec(text);
  if (!m) throw new Error(`"${text}" is not a duration such as 30s, 15m, 2h, 7d or 1w.`);
  return Number(m[1]) * (UNITS[m[2] as string] as number);
}
