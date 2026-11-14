import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { fakeClient, jsonResponse, makeWrapper } from '../test/render';
import { csrfStore } from '../state/csrf';
import { useMe } from './me';

const me = {
  user: { id: 'u1', provider: 'google', email: 'a@example.com', name: 'Ann' },
  csrfToken: 'csrf-1',
  settings: { favouriteClusters: ['v:prod'], watchIdleSeconds: 30, theme: 'system' },
  maintenanceBanner: '',
  features: { kubeconfig: true, fileBrowser: true, nodeShell: false, vault: true, helmVersion: 4, modules: {} },
  limits: { fileTransferBytes: 1, logsHistoryMaxLines: 2, logsLiveBufferLines: 3 },
  vault: null,
  keyId: 'k1',
};

function run(handler: () => Response) {
  return renderHook(() => useMe(), { wrapper: makeWrapper(fakeClient({ 'GET /me': handler })) });
}

describe('useMe', () => {
  it('starts as loading', () => {
    const { result } = run(() => jsonResponse(me));
    expect(result.current).toEqual({ state: 'loading' });
  });

  it('200 → signedIn with the user', async () => {
    const { result } = run(() => jsonResponse(me));
    await waitFor(() => expect(result.current.state).toBe('signedIn'));
    expect(result.current).toEqual({ state: 'signedIn', me });
  });

  it('remembers the CSRF token for later requests', async () => {
    run(() => jsonResponse(me));
    await waitFor(() => expect(csrfStore.getState().token).toBe('csrf-1'));
  });

  it('401 → signedOut', async () => {
    const { result } = run(() =>
      jsonResponse({ error: { code: 'unauthenticated', message: 'Sign in.' } }, 401),
    );
    await waitFor(() => expect(result.current.state).toBe('signedOut'));
  });

  it('any other error → error', async () => {
    const { result } = run(() => jsonResponse({ error: { code: 'internal', message: 'x' } }, 500));
    await waitFor(() => expect(result.current.state).toBe('error'));
  });
});
