const BINARY: Record<string, number> = {
  Ki: 2 ** 10,
  Mi: 2 ** 20,
  Gi: 2 ** 30,
  Ti: 2 ** 40,
  Pi: 2 ** 50,
  Ei: 2 ** 60,
};
const DECIMAL: Record<string, number> = {
  n: 1e-9,
  u: 1e-6,
  m: 1e-3,
  k: 1e3,
  K: 1e3,
  M: 1e6,
  G: 1e9,
  T: 1e12,
  P: 1e15,
  E: 1e18,
};
const PATTERN = /^([+-]?\d+(?:\.\d+)?|[+-]?\.\d+)(?:([eE][+-]?\d+)|(Ki|Mi|Gi|Ti|Pi|Ei|[numkKMGTPE]))?$/;

/** A Kubernetes quantity (`100m`, `512Mi`, `1e3`) as a number of base units (cores, bytes); NaN when invalid. */
export function parseQuantity(q: string): number {
  const m = PATTERN.exec(q.trim());
  if (!m) return NaN;
  const value = Number(m[1]);
  if (m[2]) return value * 10 ** Number(m[2].slice(1));
  if (!m[3]) return value;
  return value * (BINARY[m[3]] ?? DECIMAL[m[3]] ?? NaN);
}

function trim(n: number): string {
  return String(Math.round(n * 10) / 10);
}

/** Bytes with binary units: `512Mi`, `1.5Gi`. */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes)) return '';
  const units = ['Ki', 'Mi', 'Gi', 'Ti', 'Pi', 'Ei'];
  if (Math.abs(bytes) < 1024) return `${trim(bytes)}B`;
  let value = bytes;
  let unit = -1;
  while (Math.abs(value) >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${trim(value)}${units[unit]}`;
}

/** CPU cores: `250m` below one core, `4` or `1.5` above. */
export function formatCores(cores: number): string {
  if (!Number.isFinite(cores)) return '';
  if (cores < 1) return `${Math.round(cores * 1000)}m`;
  return trim(cores);
}
