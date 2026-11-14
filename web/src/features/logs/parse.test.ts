import { describe, expect, it } from 'vitest';
import { normalizeLevel, parseLine } from './parse';

describe('parseLine', () => {
  it('reads a JSON object line: fields, level, message and time', () => {
    expect(parseLine('{"level":"info","msg":"started","time":"2026-11-14T12:00:00Z","port":8080}')).toEqual({
      json: true,
      fields: { level: 'info', msg: 'started', time: '2026-11-14T12:00:00Z', port: 8080 },
      level: 'INFO',
      message: 'started',
      time: '2026-11-14T12:00:00Z',
    });
  });

  it.each([
    ['level', 'lvl', 'severity', 'loglevel'],
    ['msg', 'message'],
    ['time', 'ts', 'timestamp', '@timestamp'],
  ])('knows the key names %s', (...keys) => {
    for (const key of keys) {
      const value = key.startsWith('l') || key === 'severity' ? 'warn' : 'text';
      const p = parseLine(JSON.stringify({ [key]: value }));
      expect(p.json).toBe(true);
      if (!p.json) continue;
      if (key === 'level' || key === 'lvl' || key === 'severity' || key === 'loglevel') {
        expect(p.level).toBe('WARN');
      } else if (key === 'msg' || key === 'message') {
        expect(p.message).toBe('text');
      } else {
        expect(p.time).toBe('text');
      }
    }
  });

  it('prefers the first matching key and tolerates missing ones', () => {
    const p = parseLine('{"msg":"a","message":"b"}');
    expect(p).toMatchObject({ json: true, message: 'a' });
    expect(p.json && p.level).toBeUndefined();
  });

  it('shows numbers as text and ignores a message that is not a string', () => {
    expect(parseLine('{"time":1731585600000,"msg":42}')).toMatchObject({
      json: true,
      time: '1731585600000',
      message: undefined,
    });
  });

  it.each(['plain text', '', '[1,2,3]', '"a string"', '42', 'null', '{broken', '{"a":1} trailing'])(
    '%j is a plain line',
    (text) => {
      expect(parseLine(text)).toEqual({ json: false });
    },
  );

  it('ignores whitespace around the JSON', () => {
    expect(parseLine('  {"level":"error"}  ')).toMatchObject({ json: true, level: 'ERROR' });
  });
});

describe('normalizeLevel', () => {
  it.each([
    ['info', 'INFO'],
    ['INFO', 'INFO'],
    ['Information', 'INFO'],
    ['notice', 'INFO'],
    ['warn', 'WARN'],
    ['warning', 'WARN'],
    ['WARNING', 'WARN'],
    ['err', 'ERROR'],
    ['error', 'ERROR'],
    ['fatal', 'FATAL'],
    ['critical', 'FATAL'],
    ['panic', 'FATAL'],
    ['debug', 'DEBUG'],
    ['dbg', 'DEBUG'],
    ['trace', 'TRACE'],
    [10, 'TRACE'],
    [20, 'DEBUG'],
    [30, 'INFO'],
    [40, 'WARN'],
    [50, 'ERROR'],
    [60, 'FATAL'],
    [35, 'INFO'],
    [45, 'WARN'],
  ])('%j → %s', (input, want) => {
    expect(normalizeLevel(input)).toBe(want);
  });

  it.each(['', 'loud', null, undefined, {}, [], true, 5, 70])('%j has no level', (input) => {
    expect(normalizeLevel(input)).toBeUndefined();
  });
});
