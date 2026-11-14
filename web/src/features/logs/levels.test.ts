import { describe, expect, it } from 'vitest';
import { LEVELS, filterLines, levelClass, levelLabel, sourceColor, type LineFilter } from './levels';
import type { LogLine } from './logBuffer';

const json = (o: Record<string, unknown>, extra: Partial<LogLine> = {}): LogLine => ({
  ts: '2026-11-14T12:00:00Z',
  text: JSON.stringify(o),
  ...extra,
});
const plain = (text: string): LogLine => ({ ts: '2026-11-14T12:00:00Z', text });

const lines = [
  json({ level: 'info', msg: 'a', user: 'ada' }),
  json({ level: 'error', msg: 'b', user: 'bob' }),
  json({ level: 'warn', msg: 'c', user: 'ada', n: 3 }),
  plain('plain line'),
  { ts: '', text: '… 3 lines dropped …', marker: true },
];

const texts = (ls: LogLine[]) =>
  ls.map((l) =>
    l.marker ? 'marker' : l.text.startsWith('{') ? (JSON.parse(l.text) as { msg: string }).msg : l.text,
  );

describe('levels', () => {
  it('has a text label and a class for every level, so colour is never the only signal', () => {
    expect(LEVELS).toEqual(['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL']);
    for (const l of LEVELS) {
      expect(levelLabel(l)).toBe(l);
      expect(levelClass(l)).toBe(`level-${l.toLowerCase()}`);
    }
    expect(levelClass(undefined)).toBe('level-none');
    expect(levelLabel(undefined)).toBe('');
  });
});

describe('filterLines', () => {
  const none: LineFilter = {};

  it('shows everything without a filter', () => {
    expect(texts(filterLines(lines, none))).toEqual(['a', 'b', 'c', 'plain line', 'marker']);
  });

  it('a level set keeps those levels; plain lines and markers always stay', () => {
    expect(texts(filterLines(lines, { levels: new Set(['ERROR']) }))).toEqual(['b', 'plain line', 'marker']);
    expect(texts(filterLines(lines, { levels: new Set(['INFO', 'WARN']) }))).toEqual([
      'a',
      'c',
      'plain line',
      'marker',
    ]);
  });

  it('an empty level set shows no levelled line', () => {
    expect(texts(filterLines(lines, { levels: new Set() }))).toEqual(['plain line', 'marker']);
  });

  it('a field filter keeps JSON lines whose field equals the value; plain lines go', () => {
    expect(texts(filterLines(lines, { field: { key: 'user', value: 'ada' } }))).toEqual(['a', 'c', 'marker']);
    expect(texts(filterLines(lines, { field: { key: 'n', value: '3' } }))).toEqual(['c', 'marker']);
    expect(texts(filterLines(lines, { field: { key: 'missing', value: 'x' } }))).toEqual(['marker']);
  });

  it('level and field filters combine', () => {
    expect(
      texts(filterLines(lines, { levels: new Set(['WARN']), field: { key: 'user', value: 'ada' } })),
    ).toEqual(['c', 'marker']);
  });

  it('an empty field key is no filter', () => {
    expect(texts(filterLines(lines, { field: { key: '', value: 'x' } }))).toEqual([
      'a',
      'b',
      'c',
      'plain line',
      'marker',
    ]);
  });
});

describe('sourceColor', () => {
  it('is stable and one of twelve', () => {
    const a = sourceColor('web-1', 'app');
    expect(sourceColor('web-1', 'app')).toBe(a);
    expect(a).toBeGreaterThanOrEqual(0);
    expect(a).toBeLessThan(12);
    const seen = new Set<number>();
    for (let i = 0; i < 40; i++) seen.add(sourceColor(`pod-${i}`, 'app'));
    expect(seen.size).toBeGreaterThan(5);
    for (const c of seen) {
      expect(c).toBeLessThan(12);
    }
  });

  it('varies between the containers of one pod', () => {
    const colors = ['app', 'sidecar', 'proxy', 'init', 'metrics'].map((c) => sourceColor('web-1', c));
    expect(new Set(colors).size).toBeGreaterThan(2);
  });
});
