import { describe, expect, it } from 'vitest';
import { matchExpressions, matchLabels, matchSelector } from './selectors';

const labels = { app: 'web', tier: 'front' };

describe('matchLabels', () => {
  it.each([
    [{ app: 'web' }, true],
    [{ app: 'web', tier: 'front' }, true],
    [{ app: 'db' }, false],
    [{ other: 'x' }, false],
    [{}, true],
    [undefined, true],
  ])('%j → %s', (selector, want) => expect(matchLabels(selector, labels)).toBe(want));

  it('a missing label set matches only the empty selector', () => {
    expect(matchLabels({ app: 'web' }, undefined)).toBe(false);
    expect(matchLabels({}, undefined)).toBe(true);
  });
});

describe('matchExpressions', () => {
  it.each([
    [{ key: 'app', operator: 'In', values: ['web', 'api'] }, true],
    [{ key: 'app', operator: 'In', values: ['db'] }, false],
    [{ key: 'nope', operator: 'In', values: ['x'] }, false],
    [{ key: 'app', operator: 'In' }, false],
    [{ key: 'app', operator: 'NotIn', values: ['db'] }, true],
    [{ key: 'app', operator: 'NotIn', values: ['web'] }, false],
    [{ key: 'nope', operator: 'NotIn', values: ['web'] }, true],
    [{ key: 'app', operator: 'Exists' }, true],
    [{ key: 'nope', operator: 'Exists' }, false],
    [{ key: 'nope', operator: 'DoesNotExist' }, true],
    [{ key: 'app', operator: 'DoesNotExist' }, false],
    [{ key: 'app', operator: 'Weird' }, false],
  ] as const)('%j → %s', (req, want) => {
    expect(matchExpressions([req as never], labels)).toBe(want);
  });

  it('all requirements must hold; none holds trivially', () => {
    expect(
      matchExpressions(
        [
          { key: 'app', operator: 'Exists' },
          { key: 'x', operator: 'Exists' },
        ],
        labels,
      ),
    ).toBe(false);
    expect(matchExpressions(undefined, labels)).toBe(true);
    expect(matchExpressions([{ key: 'app', operator: 'Exists' }], undefined)).toBe(false);
  });
});

describe('matchSelector', () => {
  it('combines labels and expressions', () => {
    expect(
      matchSelector(
        {
          matchLabels: { app: 'web' },
          matchExpressions: [{ key: 'tier', operator: 'In', values: ['front'] }],
        },
        labels,
      ),
    ).toBe(true);
    expect(
      matchSelector(
        {
          matchLabels: { app: 'web' },
          matchExpressions: [{ key: 'tier', operator: 'In', values: ['back'] }],
        },
        labels,
      ),
    ).toBe(false);
    expect(matchSelector(undefined, labels)).toBe(true);
  });
});
