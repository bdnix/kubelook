import { isDuration, parseDuration } from './duration';
import type { Filter, Term } from './parse';

type Step = { key: string } | { index: number } | { star: true };

function steps(path: string): Step[] {
  const out: Step[] = [];
  for (const part of path.split('.')) {
    const m = /^([^[]*)((?:\[[^\]]*\])*)$/.exec(part);
    if (!m) continue;
    if (m[1]) out.push({ key: m[1] });
    for (const b of (m[2] ?? '').matchAll(/\[([^\]]*)\]/g)) {
      out.push(b[1] === '*' ? { star: true } : { index: Number(b[1]) });
    }
  }
  return out;
}

/**
 * The values a path leads to. A missing key gives `undefined`; `[*]` gives every element of an array, and
 * nothing at all when the value is not an array.
 */
export function resolvePath(obj: unknown, path: string): unknown[] {
  let cur: unknown[] = [obj];
  for (const step of steps(path)) {
    const next: unknown[] = [];
    for (const v of cur) {
      if ('key' in step) {
        const isObject = v !== null && typeof v === 'object' && !Array.isArray(v);
        next.push(isObject ? (v as Record<string, unknown>)[step.key] : undefined);
      } else if ('index' in step) {
        next.push(Array.isArray(v) ? (v as unknown[])[step.index] : undefined);
      } else if (Array.isArray(v)) {
        next.push(...(v as unknown[]));
      }
    }
    cur = next;
  }
  return cur;
}

const isScalar = (v: unknown): v is string | number | boolean =>
  typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean';
const isNumeric = (t: string) => t.trim() !== '' && Number.isFinite(Number(t));

/** Equal as numbers when both sides read as numbers, otherwise equal as text. */
function equals(v: unknown, text: string): boolean {
  if (!isScalar(v)) return false;
  const s = String(v);
  return isNumeric(s) && isNumeric(text) ? Number(s) === Number(text) : s === text;
}

export interface Compiled {
  test(obj: unknown): boolean;
  /** The regular-expression conditions that do not compile; they filter nothing. */
  invalid: Term[];
}

function compileTerm(t: Term, now: number, invalid: Term[]): (obj: unknown) => boolean {
  const some = (obj: unknown, fn: (v: unknown) => boolean) => resolvePath(obj, t.path).some(fn);
  const value = t.value ?? '';
  switch (t.op) {
    case '=':
      return (o) => some(o, (v) => equals(v, value));
    case '!=':
      return (o) => some(o, (v) => !equals(v, value));
    case 'contains':
      return (o) => some(o, (v) => isScalar(v) && String(v).includes(value));
    case 'regex': {
      let re: RegExp;
      try {
        re = new RegExp(value);
      } catch {
        invalid.push(t);
        return () => true;
      }
      return (o) => some(o, (v) => isScalar(v) && re.test(String(v)));
    }
    case 'in':
      return (o) => some(o, (v) => (t.values ?? []).some((x) => equals(v, x)));
    case 'exists':
      return (o) => some(o, (v) => v !== undefined && v !== null);
    default: {
      const cutoff = isDuration(value) ? now - parseDuration(value) : Date.parse(value);
      const older = t.op === 'older than';
      return (o) =>
        some(o, (v) => {
          if (typeof v !== 'string' || !/^\d{4}-\d{2}-\d{2}/.test(v)) return false; // Date.parse is too forgiving
          const at = Date.parse(v);
          if (Number.isNaN(at)) return false;
          return older ? at < cutoff : at > cutoff;
        });
    }
  }
}

/** Prepares a filter for many objects; `now` (milliseconds) is what "older than 7d" counts back from. */
export function compileFilter(filter: Filter, now: number): Compiled {
  const invalid: Term[] = [];
  const groups = filter.map((group) => group.map((t) => compileTerm(t, now, invalid)));
  return {
    invalid,
    test: (obj) => groups.length === 0 || groups.some((g) => g.every((fn) => fn(obj))),
  };
}
