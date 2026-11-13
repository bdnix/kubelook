import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { fakeClient, jsonResponse, makeWrapper } from '../test/render';
import { useMe } from './me';

const me = { id: 'u1', email: 'a@example.com', name: 'Ann' };

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
