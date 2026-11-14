import { describe, expect, it } from 'vitest';
import { createCsrfStore } from './csrf';

describe('csrf store', () => {
  it('starts without a token and keeps the one it is given', () => {
    const store = createCsrfStore();
    expect(store.getState().token).toBeUndefined();
    store.getState().setToken('abc');
    expect(store.getState().token).toBe('abc');
  });
});
