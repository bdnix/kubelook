import { describe, expect, it } from 'vitest';
import { formatBytes, formatCores, parseQuantity } from './quantity';

describe('parseQuantity', () => {
  it.each([
    ['100m', 0.1],
    ['1', 1],
    ['1.5', 1.5],
    ['.5', 0.5],
    ['512Mi', 512 * 2 ** 20],
    ['2Gi', 2 * 2 ** 30],
    ['1Ki', 1024],
    ['1e3', 1000],
    ['1E3', 1000],
    ['2e-1', 0.2],
    ['5k', 5000],
    ['5K', 5000],
    ['3M', 3e6],
    ['1G', 1e9],
    ['1E', 1e18],
    ['10u', 1e-5],
    ['100n', 1e-7],
    [' 4 ', 4],
    ['-1', -1],
  ])('%s → %s', (input, expected) => {
    expect(parseQuantity(input)).toBeCloseTo(expected, 12);
  });

  it.each(['', 'abc', '12x', '1.2.3', 'Mi', '1 Gi'])('%j is not a quantity', (input) => {
    expect(parseQuantity(input)).toBeNaN();
  });
});

describe('formatting', () => {
  it.each([
    [0, '0B'],
    [512, '512B'],
    [1024, '1Ki'],
    [1536, '1.5Ki'],
    [512 * 2 ** 20, '512Mi'],
    [1.5 * 2 ** 30, '1.5Gi'],
    [3 * 2 ** 40, '3Ti'],
    [2 ** 70, '1024Ei'],
  ])('formatBytes(%s) = %s', (n, text) => expect(formatBytes(n)).toBe(text));

  it.each([
    [0.1, '100m'],
    [0.25, '250m'],
    [1, '1'],
    [1.5, '1.5'],
    [4, '4'],
  ])('formatCores(%s) = %s', (n, text) => expect(formatCores(n)).toBe(text));

  it('non-finite numbers format as empty text', () => {
    expect(formatBytes(NaN)).toBe('');
    expect(formatCores(NaN)).toBe('');
  });
});
