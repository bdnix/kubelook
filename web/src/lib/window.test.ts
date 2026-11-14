import { describe, expect, it } from 'vitest';
import { visibleRange } from './window';

const base = { rowCount: 1000, rowHeight: 20, viewportHeight: 200, scrollTop: 0, overscan: 0 };

describe('visibleRange', () => {
  it('starts at the top', () => {
    expect(visibleRange(base)).toEqual([0, 10]);
  });

  it('follows the scroll position and adds overscan on both sides', () => {
    expect(visibleRange({ ...base, scrollTop: 400 })).toEqual([20, 30]);
    expect(visibleRange({ ...base, scrollTop: 400, overscan: 5 })).toEqual([15, 35]);
  });

  it('includes a partly visible row', () => {
    expect(visibleRange({ ...base, scrollTop: 410 })).toEqual([20, 31]);
  });

  it('clamps at both ends', () => {
    expect(visibleRange({ ...base, overscan: 5 })).toEqual([0, 15]);
    expect(visibleRange({ ...base, scrollTop: 19_900, overscan: 5 })).toEqual([990, 1000]);
  });

  it('handles an empty list and scrolling past the end', () => {
    expect(visibleRange({ ...base, rowCount: 0 })).toEqual([0, 0]);
    expect(visibleRange({ ...base, rowHeight: 0 })).toEqual([0, 0]);
    expect(visibleRange({ ...base, rowCount: 5, scrollTop: 5000 })).toEqual([5, 5]);
    expect(visibleRange({ ...base, scrollTop: -50 })).toEqual([0, 10]);
  });
});
