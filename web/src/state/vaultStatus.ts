import { useStore } from 'zustand';
import { createStore } from 'zustand/vanilla';

/** What the server says about the Vault token (the `VaultStatus` of the credentials API). */
export interface VaultStatus {
  displayName: string;
  entityId: string;
  expiresAt: string | null;
  ttlSeconds: number;
  renewable: boolean;
  canRenew: boolean;
  neverExpires: boolean;
  warning: boolean;
  expired: boolean;
}

export interface VaultStatusState {
  status: VaultStatus | null;
  set: (status: VaultStatus) => void;
  clear: () => void;
}

export function createVaultStatusStore() {
  return createStore<VaultStatusState>()((set) => ({
    status: null,
    set: (status) => set({ status }),
    clear: () => set({ status: null }),
  }));
}

export const vaultStatusStore = createVaultStatusStore();

export function useVaultStatus(): VaultStatusState {
  return useStore(vaultStatusStore);
}
