import { createContext, useContext } from 'react';
import type { VaultStore } from './vaultStore';

export const VaultStoreContext = createContext<VaultStore | null>(null);

/** The saved-copy store of the signed-in user. */
export function useVaultStore(): VaultStore {
  const store = useContext(VaultStoreContext);
  if (!store) throw new Error('VaultStoreContext is missing: render inside the signed-in app');
  return store;
}
