import { useStore } from 'zustand';
import { createStore } from 'zustand/vanilla';

export interface CsrfState {
  token: string | undefined;
  setToken: (token: string) => void;
}

export function createCsrfStore() {
  return createStore<CsrfState>()((set) => ({
    token: undefined,
    setToken: (token) => set({ token }),
  }));
}

/** The CSRF token of the current session; the API client reads it for every write. */
export const csrfStore = createCsrfStore();

export function useCsrf(): CsrfState {
  return useStore(csrfStore);
}
