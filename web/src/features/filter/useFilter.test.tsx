import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useFilter } from './useFilter';

const NOW = Date.parse('2026-11-14T12:00:00Z');
const items = [
  { metadata: { name: 'a' }, status: { phase: 'Running' } },
  { metadata: { name: 'b' }, status: { phase: 'Pending' } },
];
const kind = { group: '', kind: 'Pod' };

describe('useFilter', () => {
  it('returns every row without a query', () => {
    const { result } = renderHook(() => useFilter(items, '', kind, NOW));
    expect(result.current.rows).toBe(items);
    expect(result.current.filter).toEqual([]);
  });

  it('keeps the rows that match', () => {
    const { result } = renderHook(() => useFilter(items, 'status.phase = Running', kind, NOW));
    expect(result.current.rows.map((r) => (r.metadata as { name: string }).name)).toEqual(['a']);
  });

  it('a query that does not parse reports the error and filters nothing', () => {
    const { result } = renderHook(() => useFilter(items, 'status.phase =', kind, NOW));
    expect(result.current.error?.position).toBe(14);
    expect(result.current.rows).toBe(items);
  });

  it('an invalid regular expression is listed and filters nothing', () => {
    const { result } = renderHook(() => useFilter(items, 'metadata.name regex (', kind, NOW));
    expect(result.current.invalid).toEqual(['metadata.name regex (']);
    expect(result.current.rows).toHaveLength(2);
  });

  it('Secret values cannot be filtered', () => {
    const { result } = renderHook(() => useFilter(items, 'data.x = y', { group: '', kind: 'Secret' }, NOW));
    expect(result.current.secretError).toBe("Secret values can't be filtered.");
    expect(result.current.rows).toBe(items);
  });

  it('does not evaluate again while the list and the query are the same', () => {
    const { result, rerender } = renderHook(({ q }) => useFilter(items, q, kind, NOW), {
      initialProps: { q: 'status.phase = Running' },
    });
    const first = result.current.rows;
    rerender({ q: 'status.phase = Running' });
    expect(result.current.rows).toBe(first);
    rerender({ q: 'status.phase = Pending' });
    expect(result.current.rows).not.toBe(first);
  });

  it('evaluates again when the list changes', () => {
    const { result, rerender } = renderHook(
      ({ list }) => useFilter(list, 'status.phase = Running', kind, NOW),
      {
        initialProps: { list: items },
      },
    );
    expect(result.current.rows).toHaveLength(1);
    rerender({ list: [...items, { metadata: { name: 'c' }, status: { phase: 'Running' } }] });
    expect(result.current.rows).toHaveLength(2);
  });
});
