import { isDuration } from './duration';

export type Operator = '=' | '!=' | 'contains' | 'regex' | 'in' | 'exists' | 'older than' | 'newer than';

/** One condition. `value` goes with every operator but `in` and `exists`; `values` goes with `in`. */
export interface Term {
  path: string;
  op: Operator;
  value?: string;
  values?: string[];
}

/** OR of ANDs: the inner lists are joined with AND, the outer list with OR (AND binds tighter). */
export type Filter = Term[][];

export interface ParseError {
  /** The index in the text where the problem starts. */
  position: number;
  message: string;
}

export type ParseOutcome = { ok: true; filter: Filter } | { ok: false; error: ParseError };

class Failure extends Error {
  constructor(
    readonly position: number,
    message: string,
  ) {
    super(message);
  }
}

const WORD_OPS = new Set(['contains', 'regex', 'in', 'exists', 'older', 'newer']);
const isSpace = (c: string | undefined) => c !== undefined && /\s/.test(c);
const isName = (c: string | undefined) => c !== undefined && /[A-Za-z0-9_-]/.test(c);

class Parser {
  i = 0;
  constructor(private readonly s: string) {}

  private skip() {
    while (isSpace(this.s[this.i])) this.i++;
  }
  private atEnd() {
    this.skip();
    return this.i >= this.s.length;
  }
  private word(): string {
    const start = this.i;
    while (this.i < this.s.length && /[A-Za-z]/.test(this.s[this.i] as string)) this.i++;
    return this.s.slice(start, this.i);
  }

  parse(): Filter {
    if (this.atEnd()) throw new Failure(this.s.length, 'Enter a filter, for example status.phase = Running.');
    const groups: Term[][] = [[]];
    for (;;) {
      (groups[groups.length - 1] as Term[]).push(this.term());
      if (this.atEnd()) return groups;
      const at = this.i;
      const kw = this.word().toUpperCase();
      if (kw === 'OR') groups.push([]);
      else if (kw !== 'AND') throw new Failure(at, 'Expected AND or OR here.');
      if (this.atEnd()) throw new Failure(this.s.length, `Expected a field after ${kw}.`);
    }
  }

  private term(): Term {
    const path = this.path();
    this.skip();
    const at = this.i;
    if (this.s.startsWith('!=', this.i)) {
      this.i += 2;
      return { path, op: '!=', value: this.value() };
    }
    if (this.s[this.i] === '=') {
      this.i++;
      return { path, op: '=', value: this.value() };
    }
    const w = this.word().toLowerCase();
    if (!WORD_OPS.has(w)) {
      this.i = at;
      throw new Failure(
        at,
        'Expected an operator: =, !=, contains, regex, in, exists, older than or newer than.',
      );
    }
    switch (w) {
      case 'exists':
        return { path, op: 'exists' };
      case 'in':
        return { path, op: 'in', values: this.list() };
      case 'contains':
      case 'regex':
        return { path, op: w, value: this.value() };
      default: {
        this.skip();
        const thanAt = this.i;
        if (this.word().toLowerCase() !== 'than') throw new Failure(thanAt, `Expected "than" after ${w}.`);
        const op = w === 'older' ? 'older than' : 'newer than';
        const value = this.value('Expected a duration like 7d or a timestamp.');
        const startOfValue = this.i - value.length; // a bare value; quoted ones are rare for times
        if (!isDuration(value) && !(/^\d{4}-\d{2}-\d{2}/.test(value) && !Number.isNaN(Date.parse(value)))) {
          throw new Failure(Math.max(startOfValue, 0), 'Expected a duration like 7d or a timestamp.');
        }
        return { path, op, value };
      }
    }
  }

  /** A dotted path with [n] and [*] after names; a malformed path is reported at its start. */
  private path(): string {
    this.skip();
    const start = this.i;
    const bad = () =>
      new Failure(start, 'Expected a field name such as status.phase or spec.containers[*].image.');
    const first = this.s.slice(this.i).match(/^[A-Za-z0-9_-]+/);
    if (!first || /^(and|or)$/i.test(first[0])) throw bad();
    for (;;) {
      if (!isName(this.s[this.i])) throw bad();
      while (isName(this.s[this.i])) this.i++;
      while (this.s[this.i] === '[') {
        const m = /^\[(\*|\d+)\]/.exec(this.s.slice(this.i));
        if (!m) throw bad();
        this.i += m[0].length;
      }
      if (this.s[this.i] !== '.') break;
      this.i++;
    }
    return this.s.slice(start, this.i);
  }

  /** A quoted string or a bare word; bare words stop at white space and, in lists, at `,` and `)`. */
  private value(missing = 'Expected a value.', inList = false): string {
    this.skip();
    if (this.i >= this.s.length) throw new Failure(this.s.length, missing);
    if (this.s[this.i] === '"') {
      const open = this.i++;
      let out = '';
      while (this.i < this.s.length) {
        const c = this.s[this.i] as string;
        if (c === '\\' && (this.s[this.i + 1] === '"' || this.s[this.i + 1] === '\\')) {
          out += this.s[this.i + 1];
          this.i += 2;
        } else if (c === '"') {
          this.i++;
          return out;
        } else {
          out += c;
          this.i++;
        }
      }
      throw new Failure(open, 'The quote is not closed.');
    }
    const start = this.i;
    while (
      this.i < this.s.length &&
      !isSpace(this.s[this.i]) &&
      !(inList && (this.s[this.i] === ',' || this.s[this.i] === ')'))
    ) {
      this.i++;
    }
    if (this.i === start) throw new Failure(start, missing);
    return this.s.slice(start, this.i);
  }

  private list(): string[] {
    this.skip();
    if (this.s[this.i] !== '(')
      throw new Failure(Math.min(this.i, this.s.length), 'Expected ( to start the list of values.');
    this.i++;
    const values: string[] = [];
    for (;;) {
      values.push(this.value('Expected a value.', true));
      this.skip();
      if (this.s[this.i] === ',') {
        this.i++;
        continue;
      }
      if (this.s[this.i] === ')') {
        this.i++;
        return values;
      }
      throw new Failure(Math.min(this.i, this.s.length), 'Expected , or ) in the list of values.');
    }
  }
}

/** Parses the text of the query bar. */
export function parseFilter(text: string): ParseOutcome {
  const p = new Parser(text);
  try {
    return { ok: true, filter: p.parse() };
  } catch (e) {
    if (e instanceof Failure) return { ok: false, error: { position: e.position, message: e.message } };
    throw e;
  }
}

const needsQuotes = (v: string, inList: boolean) =>
  v === '' || /[\s"]/.test(v) || /^(and|or)$/i.test(v) || (inList && /[,()]/.test(v));
const quoted = (v: string) => `"${v.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
const text = (v: string, inList = false) => (needsQuotes(v, inList) ? quoted(v) : v);

/** One condition as text. */
export function formatTerm(t: Term): string {
  switch (t.op) {
    case 'exists':
      return `${t.path} exists`;
    case 'in':
      return `${t.path} in (${(t.values ?? []).map((v) => text(v, true)).join(', ')})`;
    default:
      return `${t.path} ${t.op} ${text(t.value ?? '')}`;
  }
}

/** The filter as text that parses back to the same filter. */
export function formatFilter(filter: Filter): string {
  return filter.map((group) => group.map(formatTerm).join(' AND ')).join(' OR ');
}

/** A condition in the rows editor, with the join that connects it to the one before. */
export type Row = Term & { join: 'AND' | 'OR' };

export function filterToRows(filter: Filter): Row[] {
  return filter.flatMap((group, g) =>
    group.map((term, i): Row => ({ ...term, join: g > 0 && i === 0 ? 'OR' : 'AND' })),
  );
}

const complete = (r: Term): boolean =>
  r.path !== '' &&
  (r.op === 'exists' || (r.op === 'in' ? (r.values ?? []).length > 0 : (r.value ?? '') !== ''));

/** The filter of the complete rows; an incomplete row is left out. */
export function rowsToFilter(rows: Row[]): Filter {
  const groups: Term[][] = [];
  for (const { join, ...term } of rows) {
    if (!complete(term)) continue;
    if (groups.length === 0 || join === 'OR') groups.push([]);
    (groups[groups.length - 1] as Term[]).push(term);
  }
  return groups;
}
