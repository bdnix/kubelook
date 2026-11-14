/** Reads a dotted path such as `status.phase` from an object; `undefined` when any step is missing. */
export function get(obj: unknown, path: string): unknown {
  let cur: unknown = obj;
  for (const key of path.split('.')) {
    if (cur === null || typeof cur !== 'object') return undefined;
    cur = (cur as Record<string, unknown>)[key];
  }
  return cur;
}

export function getString(obj: unknown, path: string, fallback = ''): string {
  const v = get(obj, path);
  return typeof v === 'string' ? v : fallback;
}

export function getNumber(obj: unknown, path: string, fallback = 0): number {
  const v = get(obj, path);
  return typeof v === 'number' ? v : fallback;
}

export function getArray(obj: unknown, path: string): unknown[] {
  const v = get(obj, path);
  return Array.isArray(v) ? v : [];
}
