/**
 * The subset of JSONPath that `additionalPrinterColumns` of CRDs use: `.a.b`, `.a[0]`, `.a[*].b`,
 * `.a[?(@.type=="Ready")].status` and `.a['x\.y']`. Anything else gives an empty string, never an error.
 */

type Step =
  | { kind: 'key'; key: string }
  | { kind: 'index'; index: number }
  | { kind: 'all' }
  | { kind: 'filter'; path: string[]; expected: string };

const NAME = /[A-Za-z0-9_$-]+/y;
const INDEX = /\[(\d+)\]/y;
const ALL = /\[\*\]/y;
const FILTER = /\[\?\(@((?:\.[A-Za-z0-9_$-]+)+)\s*==\s*(?:"([^"]*)"|'([^']*)'|(-?\d+(?:\.\d+)?|true|false))\)\]/y;

function match(re: RegExp, s: string, at: number): RegExpExecArray | null {
  re.lastIndex = at;
  return re.exec(s);
}

/** Reads `['key']` or `["key"]`; a backslash makes the next character literal, so `\.` is a plain dot. */
function quotedKey(s: string, at: number): { key: string; end: number } | undefined {
  const quote = s[at + 1];
  if (s[at] !== '[' || (quote !== "'" && quote !== '"')) return undefined;
  let key = '';
  for (let i = at + 2; i < s.length; i++) {
    const c = s[i] as string;
    if (c === '\\') {
      i++;
      if (i >= s.length) return undefined;
      key += s[i];
    } else if (c === quote) {
      return s[i + 1] === ']' ? { key, end: i + 2 } : undefined;
    } else {
      key += c;
    }
  }
  return undefined;
}

function parse(path: string): Step[] | undefined {
  const steps: Step[] = [];
  let i = path.startsWith('.') ? 1 : 0;
  if (path.length === i) return undefined;
  let first = true;
  while (i < path.length) {
    const c = path[i];
    if (c === '[') {
      const quoted = quotedKey(path, i);
      let m: RegExpExecArray | null;
      if (quoted) {
        steps.push({ kind: 'key', key: quoted.key });
        i = quoted.end;
      } else if ((m = match(INDEX, path, i))) {
        steps.push({ kind: 'index', index: Number(m[1]) });
        i += m[0].length;
      } else if ((m = match(ALL, path, i))) {
        steps.push({ kind: 'all' });
        i += m[0].length;
      } else if ((m = match(FILTER, path, i))) {
        const expected = m[2] ?? m[3] ?? m[4] ?? '';
        steps.push({ kind: 'filter', path: (m[1] as string).slice(1).split('.'), expected });
        i += m[0].length;
      } else {
        return undefined;
      }
    } else {
      // a name: after the leading dot, or after a dot that follows another step
      if (!first) {
        if (c !== '.') return undefined;
        i++;
      }
      const m = match(NAME, path, i);
      if (!m) return undefined;
      steps.push({ kind: 'key', key: m[0] });
      i += m[0].length;
    }
    first = false;
  }
  return steps;
}

function child(v: unknown, key: string): unknown {
  return v !== null && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>)[key] : undefined;
}

function at(v: unknown, path: string[]): unknown {
  return path.reduce<unknown>((cur, k) => child(cur, k), v);
}

function apply(nodes: unknown[], step: Step): unknown[] {
  const out: unknown[] = [];
  for (const n of nodes) {
    if (step.kind === 'key') out.push(child(n, step.key));
    else if (!Array.isArray(n)) continue;
    else if (step.kind === 'index') out.push(n[step.index]);
    else if (step.kind === 'all') out.push(...n);
    else out.push(...n.filter((e) => String(at(e, step.path)) === step.expected));
  }
  return out.filter((v) => v !== undefined && v !== null);
}

function show(v: unknown): string {
  if (typeof v === 'string') return v;
  if (typeof v === 'number' || typeof v === 'boolean') return String(v);
  return JSON.stringify(v);
}

/** The text of `path` evaluated on `obj`; several matches are joined with a comma. */
export function evaluateJsonPath(obj: unknown, path: string): string {
  const steps = parse(path);
  if (!steps) return '';
  let nodes: unknown[] = [obj];
  for (const step of steps) nodes = apply(nodes, step);
  return nodes.map(show).join(',');
}
