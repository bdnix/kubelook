import { createContext, useContext } from 'react';
import { csrfStore } from '../state/csrf';
import { browserClient, type ApiClient } from './client';

/** The client the running app uses: the browser's fetch, with the session's CSRF token on every write. */
export function appClient(): ApiClient {
  return browserClient(() => csrfStore.getState().token);
}

export const ApiContext = createContext<ApiClient>(appClient());

export function useApi(): ApiClient {
  return useContext(ApiContext);
}
