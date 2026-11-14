import { describe, expect, it } from 'vitest';
import { formatAge } from './age';

const now = Date.parse('2026-11-14T12:00:00Z');
const ago = (seconds: number) => new Date(now - seconds * 1000).toISOString();

describe('formatAge', () => {
  it.each([
    [0, '0s'],
    [45, '45s'],
    [60, '1m'],
    [12 * 60 + 5, '12m'],
    [3 * 3600 + 59, '3h'],
    [2 * 86400, '2d'],
    [5 * 7 * 86400, '5w'],
    [400 * 86400, '1y'],
  ])('%i seconds → %s', (seconds, text) => {
    expect(formatAge(ago(seconds), now)).toBe(text);
  });

  it('is empty without a valid timestamp and never negative', () => {
    expect(formatAge(undefined, now)).toBe('');
    expect(formatAge('nonsense', now)).toBe('');
    expect(formatAge(new Date(now + 5000).toISOString(), now)).toBe('0s');
  });
});
