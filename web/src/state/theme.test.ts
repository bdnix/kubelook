import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { createThemeStore, themeStore, useTheme } from './theme';

function fakeStorage(initial: Record<string, string> = {}) {
  const data = { ...initial };
  return {
    data,
    getItem: (k: string) => data[k] ?? null,
    setItem: (k: string, v: string) => {
      data[k] = v;
    },
  };
}

describe('theme store', () => {
  it('defaults to system and sets no data-theme', () => {
    const root = document.createElement('html');
    const store = createThemeStore({ storage: fakeStorage(), root });
    expect(store.getState().theme).toBe('system');
    expect(root.dataset['theme']).toBeUndefined();
  });

  it('setTheme(dark) sets data-theme and persists to kubelook.theme', () => {
    const root = document.createElement('html');
    const storage = fakeStorage();
    const store = createThemeStore({ storage, root });
    store.getState().setTheme('dark');
    expect(store.getState().theme).toBe('dark');
    expect(root.dataset['theme']).toBe('dark');
    expect(storage.data['kubelook.theme']).toBe('dark');
  });

  it('setTheme(system) removes data-theme again', () => {
    const root = document.createElement('html');
    const store = createThemeStore({ storage: fakeStorage(), root });
    store.getState().setTheme('light');
    expect(root.dataset['theme']).toBe('light');
    store.getState().setTheme('system');
    expect(root.dataset['theme']).toBeUndefined();
  });

  it('applies a stored theme when created', () => {
    const root = document.createElement('html');
    const store = createThemeStore({ storage: fakeStorage({ 'kubelook.theme': 'dark' }), root });
    expect(store.getState().theme).toBe('dark');
    expect(root.dataset['theme']).toBe('dark');
  });

  it('ignores an invalid stored value', () => {
    const root = document.createElement('html');
    const store = createThemeStore({ storage: fakeStorage({ 'kubelook.theme': 'neon' }), root });
    expect(store.getState().theme).toBe('system');
  });

  it('keeps working when storage throws', () => {
    const root = document.createElement('html');
    const broken = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    };
    const store = createThemeStore({ storage: broken, root });
    expect(store.getState().theme).toBe('system');
    store.getState().setTheme('dark');
    expect(store.getState().theme).toBe('dark');
    expect(root.dataset['theme']).toBe('dark');
  });
});

describe('useTheme', () => {
  it('reads and updates the shared store', () => {
    const { result } = renderHook(() => useTheme());
    expect(result.current.theme).toBe('system');
    act(() => result.current.setTheme('dark'));
    expect(result.current.theme).toBe('dark');
    expect(document.documentElement.dataset['theme']).toBe('dark');
    act(() => themeStore.getState().setTheme('system'));
    expect(document.documentElement.dataset['theme']).toBeUndefined();
  });
});
