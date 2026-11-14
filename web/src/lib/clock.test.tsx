import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useNow } from './clock';

describe('useNow', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-11-14T12:00:00Z'));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns the current time and updates on the interval', () => {
    const { result } = renderHook(() => useNow(60_000));
    expect(result.current).toBe(Date.parse('2026-11-14T12:00:00Z'));
    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(result.current).toBe(Date.parse('2026-11-14T12:01:00Z'));
  });

  it('stops ticking when unmounted', () => {
    const { unmount } = renderHook(() => useNow(60_000));
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
