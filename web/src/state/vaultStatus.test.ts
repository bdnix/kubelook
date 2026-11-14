import { describe, expect, it } from 'vitest';
import { createVaultStatusStore, type VaultStatus } from './vaultStatus';

const status: VaultStatus = {
  displayName: 'ada',
  entityId: 'e1',
  expiresAt: '2026-11-14T12:30:00Z',
  ttlSeconds: 1800,
  renewable: true,
  canRenew: true,
  neverExpires: false,
  warning: false,
  expired: false,
};

describe('vault status store', () => {
  it('starts empty, holds a status and clears it again', () => {
    const store = createVaultStatusStore();
    expect(store.getState().status).toBeNull();
    store.getState().set(status);
    expect(store.getState().status).toEqual(status);
    store.getState().clear();
    expect(store.getState().status).toBeNull();
  });
});
