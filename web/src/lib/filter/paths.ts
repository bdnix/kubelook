import type { Filter } from './parse';

export const SECRET_MESSAGE = "Secret values can't be filtered.";

const MAX_OBJECTS = 50;
const MAX_DEPTH = 6;
const KEY = /^[A-Za-z0-9_-]+$/;

interface Kind {
  group: string;
  kind: string;
}

const isSecret = (k: Kind) => k.group === '' && k.kind === 'Secret';

function walk(value: unknown, prefix: string, depth: number, out: Set<string>, secret: boolean): void {
  if (Array.isArray(value)) {
    const elements = value as unknown[];
    if (elements.some((e) => e !== null && typeof e !== 'object')) out.add(`${prefix}[*]`);
    for (const e of elements) {
      if (e !== null && typeof e === 'object' && !Array.isArray(e))
        walk(e, `${prefix}[*]`, depth, out, false);
    }
    return;
  }
  if (value === null || typeof value !== 'object') return;
  for (const [key, v] of Object.entries(value as Record<string, unknown>)) {
    if (!KEY.test(key) || depth + 1 > MAX_DEPTH) continue;
    if (secret && (key === 'data' || key === 'stringData')) continue;
    const path = prefix === '' ? key : `${prefix}.${key}`;
    out.add(path);
    walk(v, path, depth + 1, out, false);
  }
}

/** The paths to suggest in the filter: from the first 50 live objects, six levels deep, arrays as `[*]`. */
export function collectPaths(objects: unknown[], kind: Kind): string[] {
  const out = new Set<string>();
  for (const o of objects.slice(0, MAX_OBJECTS)) walk(o, '', 0, out, isSecret(kind));
  return [...out].sort();
}

/** The message to show when the filter reads the values of a Secret; undefined when it is fine. */
export function secretViolation(filter: Filter, kind: Kind): string | undefined {
  if (!isSecret(kind)) return undefined;
  const touches = filter.some((group) => group.some((t) => /^(data|stringData)(\.|\[|$)/.test(t.path)));
  return touches ? SECRET_MESSAGE : undefined;
}
