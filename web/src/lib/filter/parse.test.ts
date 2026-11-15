import { describe, expect, it } from 'vitest';
import { filterToRows, formatFilter, parseFilter, rowsToFilter, type Filter } from './parse';

const ok = (text: string): Filter => {
  const r = parseFilter(text);
  if (!r.ok) throw new Error(`${text}: ${r.error.message} at ${r.error.position}`);
  return r.filter;
};
const fail = (text: string) => {
  const r = parseFilter(text);
  if (r.ok) throw new Error(`${text} should not parse`);
  return r.error;
};

describe('parseFilter: operators', () => {
  it.each([
    ['status.phase = Running', { path: 'status.phase', op: '=', value: 'Running' }],
    ['status.phase != Running', { path: 'status.phase', op: '!=', value: 'Running' }],
    ['metadata.name contains web', { path: 'metadata.name', op: 'contains', value: 'web' }],
    ['metadata.name regex ^web-\\d+$', { path: 'metadata.name', op: 'regex', value: '^web-\\d+$' }],
    ['metadata.deletionTimestamp exists', { path: 'metadata.deletionTimestamp', op: 'exists' }],
    [
      'metadata.creationTimestamp older than 7d',
      { path: 'metadata.creationTimestamp', op: 'older than', value: '7d' },
    ],
    [
      'metadata.creationTimestamp newer than 15m',
      { path: 'metadata.creationTimestamp', op: 'newer than', value: '15m' },
    ],
    [
      'metadata.creationTimestamp older than 2026-11-01T00:00:00Z',
      { path: 'metadata.creationTimestamp', op: 'older than', value: '2026-11-01T00:00:00Z' },
    ],
  ])('%s', (text, term) => {
    expect(ok(text)).toEqual([[term]]);
  });

  it('in takes a parenthesised list', () => {
    expect(ok('status.phase in (Running, Pending)')).toEqual([
      [{ path: 'status.phase', op: 'in', values: ['Running', 'Pending'] }],
    ]);
    expect(ok('a in (x)')).toEqual([[{ path: 'a', op: 'in', values: ['x'] }]]);
    expect(ok('a in ("x, y", z)')).toEqual([[{ path: 'a', op: 'in', values: ['x, y', 'z'] }]]);
  });

  it('keywords may be written in lower case, paths keep their case', () => {
    expect(ok('Spec.Name CONTAINS x and a = 1 or b EXISTS')).toEqual([
      [
        { path: 'Spec.Name', op: 'contains', value: 'x' },
        { path: 'a', op: '=', value: '1' },
      ],
      [{ path: 'b', op: 'exists' }],
    ]);
  });
});

describe('parseFilter: values and paths', () => {
  it('quoted strings may hold spaces, keywords and escaped quotes and backslashes', () => {
    expect(ok('metadata.name = "my pod"')).toEqual([[{ path: 'metadata.name', op: '=', value: 'my pod' }]]);
    expect(ok('a = "x AND y"')).toEqual([[{ path: 'a', op: '=', value: 'x AND y' }]]);
    expect(ok('a = "say \\"hi\\" \\\\ ok"')).toEqual([[{ path: 'a', op: '=', value: 'say "hi" \\ ok' }]]);
    expect(ok('a = ""')).toEqual([[{ path: 'a', op: '=', value: '' }]]);
  });

  it('bare values stop at white space', () => {
    expect(ok('a = x/y:1.2-3_4 AND b = c')).toEqual([
      [
        { path: 'a', op: '=', value: 'x/y:1.2-3_4' },
        { path: 'b', op: '=', value: 'c' },
      ],
    ]);
  });

  it('paths may index arrays by number or by [*]', () => {
    expect(ok('spec.containers[*].image = nginx')[0]?.[0]?.path).toBe('spec.containers[*].image');
    expect(ok('spec.containers[0].name = app')[0]?.[0]?.path).toBe('spec.containers[0].name');
    expect(ok('status.containerStatuses[*].state.waiting.reason = CrashLoopBackOff')[0]?.[0]?.path).toBe(
      'status.containerStatuses[*].state.waiting.reason',
    );
  });

  it('white space around is ignored', () => {
    expect(ok('  a   =   b  ')).toEqual([[{ path: 'a', op: '=', value: 'b' }]]);
  });
});

describe('parseFilter: precedence', () => {
  it('AND binds tighter than OR', () => {
    expect(ok('a = 1 OR b = 2 AND c = 3')).toEqual([
      [{ path: 'a', op: '=', value: '1' }],
      [
        { path: 'b', op: '=', value: '2' },
        { path: 'c', op: '=', value: '3' },
      ],
    ]);
    expect(ok('a = 1 AND b = 2 OR c = 3')).toEqual([
      [
        { path: 'a', op: '=', value: '1' },
        { path: 'b', op: '=', value: '2' },
      ],
      [{ path: 'c', op: '=', value: '3' }],
    ]);
  });
});

describe('parseFilter: errors carry a position and a message', () => {
  it.each([
    ['', 0, /filter/i],
    ['   ', 3, /filter/i],
    ['a =', 3, /value/i],
    ['a = ', 4, /value/i],
    ['= 1', 0, /field/i],
    ['a ~ 1', 2, /operator/i],
    ['a', 1, /operator/i],
    ['a = "open', 4, /quote/i],
    ['a = 1 AND', 9, /field/i],
    ['a = 1 b = 2', 6, /AND or OR/],
    ['a in x', 5, /\(/],
    ['a in (x, y', 10, /\)/],
    ['a in ()', 6, /value/i],
    ['a older than', 12, /duration|timestamp/i],
    ['a older than soon', 13, /duration|timestamp/i],
    ['a older 7d', 8, /than/],
    ['a[ = 1', 0, /field/i],
    ['a[x] = 1', 0, /field/i],
    ['a..b = 1', 0, /field/i],
    ['a = 1 AND OR', 10, /field/i],
  ])('%j', (text, position, message) => {
    const e = fail(text);
    expect(e.position).toBe(position);
    expect(e.message).toMatch(message);
  });
});

describe('formatFilter and rows', () => {
  it.each([
    'a = 1',
    'a = 1 AND b != 2',
    'a = 1 OR b = 2 AND c = 3',
    'spec.containers[*].image contains nginx',
    'status.phase in (Running, Pending)',
    'metadata.deletionTimestamp exists',
    'metadata.creationTimestamp older than 7d',
    'a = "my pod" AND b = "x \\"y\\""',
    'a = ""',
  ])('%s round-trips through the text', (text) => {
    expect(formatFilter(ok(text))).toBe(text);
  });

  it('normalises spacing, keyword case and needless quotes', () => {
    expect(formatFilter(ok('a   =   "b"  and  c EXISTS'))).toBe('a = b AND c exists');
  });

  it('quotes values that need it', () => {
    expect(formatFilter([[{ path: 'a', op: '=', value: 'two words' }]])).toBe('a = "two words"');
    expect(formatFilter([[{ path: 'a', op: '=', value: 'AND' }]])).toBe('a = "AND"');
    expect(formatFilter([[{ path: 'a', op: 'in', values: ['x, y', 'z'] }]])).toBe('a in ("x, y", z)');
  });

  it('rows carry the join of each condition and convert back and forth', () => {
    const filter = ok('a = 1 OR b = 2 AND c = 3');
    const rows = filterToRows(filter);
    expect(rows.map((r) => r.join)).toEqual(['AND', 'OR', 'AND']);
    expect(rows.map((r) => r.path)).toEqual(['a', 'b', 'c']);
    expect(rowsToFilter(rows)).toEqual(filter);
    expect(formatFilter(rowsToFilter(rows))).toBe('a = 1 OR b = 2 AND c = 3');
  });

  it('rows drop incomplete conditions', () => {
    expect(
      rowsToFilter([
        { join: 'AND', path: 'a', op: '=', value: '1' },
        { join: 'AND', path: '', op: '=', value: 'x' },
        { join: 'OR', path: 'b', op: '=', value: '' },
        { join: 'AND', path: 'c', op: 'exists' },
      ]),
    ).toEqual([
      [
        { path: 'a', op: '=', value: '1' },
        { path: 'c', op: 'exists' },
      ],
    ]);
  });
});
