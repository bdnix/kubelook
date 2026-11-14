import { describe, expect, it } from 'vitest';
import { isDuration, parseDuration } from './duration';

describe('parseDuration', () => {
  it.each([
    ['30s', 30_000],
    ['15m', 900_000],
    ['2h', 7_200_000],
    ['7d', 604_800_000],
    ['1w', 604_800_000],
    ['0s', 0],
    ['120m', 7_200_000],
  ])('%s is %d ms', (text, ms) => {
    expect(parseDuration(text)).toBe(ms);
    expect(isDuration(text)).toBe(true);
  });

  it.each(['', '5', 'm', '5x', '-5m', '1.5h', '5 m', '5M', 'seven days', '1d2h'])('%j is not a duration', (text) => {
    expect(() => parseDuration(text)).toThrow(/duration/i);
    expect(isDuration(text)).toBe(false);
  });
});
