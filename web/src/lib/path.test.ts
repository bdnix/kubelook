import { describe, expect, it } from 'vitest';
import { get, getArray, getNumber, getString } from './path';

const obj = { status: { phase: 'Running', restarts: 3, list: [1, 2] }, name: null };

describe('path helpers', () => {
  it('reads nested values', () => {
    expect(get(obj, 'status.phase')).toBe('Running');
    expect(getString(obj, 'status.phase')).toBe('Running');
    expect(getNumber(obj, 'status.restarts')).toBe(3);
    expect(getArray(obj, 'status.list')).toEqual([1, 2]);
  });

  it('returns undefined or the fallback when a step is missing or has the wrong type', () => {
    expect(get(obj, 'status.nope.deeper')).toBeUndefined();
    expect(get(obj, 'name.first')).toBeUndefined();
    expect(get(undefined, 'a')).toBeUndefined();
    expect(getString(obj, 'status.restarts')).toBe('');
    expect(getString(obj, 'status.nope', 'x')).toBe('x');
    expect(getNumber(obj, 'status.phase', -1)).toBe(-1);
    expect(getArray(obj, 'status.phase')).toEqual([]);
  });
});
