import { createContext, useContext } from 'react';
import { browserClient, type ApiClient } from './client';

export const ApiContext = createContext<ApiClient>(browserClient());

export function useApi(): ApiClient {
  return useContext(ApiContext);
}
