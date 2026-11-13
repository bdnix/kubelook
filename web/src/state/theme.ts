import { createStore } from 'zustand/vanilla';
import { useStore } from 'zustand';

export type Theme = 'system' | 'light' | 'dark';
const THEMES: readonly Theme[] = ['system', 'light', 'dark'];
const STORAGE_KEY = 'kubelook.theme';

interface KeyValueStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

export interface ThemeState {
  theme: Theme;
  setTheme: (theme: Theme) => void;
}

function apply(root: HTMLElement, theme: Theme): void {
  if (theme === 'system') delete root.dataset['theme'];
  else root.dataset['theme'] = theme;
}

/** The theme store. Storage is injected because it can be blocked or full. */
export function createThemeStore({ storage, root }: { storage: KeyValueStorage; root: HTMLElement }) {
  let initial: Theme = 'system';
  try {
    const stored = storage.getItem(STORAGE_KEY);
    if (THEMES.includes(stored as Theme)) initial = stored as Theme;
  } catch {
    // storage unavailable: use the default
  }
  apply(root, initial);

  return createStore<ThemeState>()((set) => ({
    theme: initial,
    setTheme: (theme) => {
      apply(root, theme);
      try {
        storage.setItem(STORAGE_KEY, theme);
      } catch {
        // not persisted; the choice still applies for this page
      }
      set({ theme });
    },
  }));
}

export const themeStore = createThemeStore({ storage: window.localStorage, root: document.documentElement });

export function useTheme(): ThemeState {
  return useStore(themeStore);
}
