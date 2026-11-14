import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { signinErrorMessage } from './signinErrors';
import { SignInPage } from './SignInPage';

function client(providers: string[]) {
  return fakeClient({ 'GET /auth/providers': () => jsonResponse({ providers }) });
}

describe('SignInPage', () => {
  it('shows the product name and one link per provider', async () => {
    renderApp(<SignInPage />, { client: client(['github', 'google']) });
    expect(screen.getByRole('heading', { level: 1, name: 'Kubelook' })).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: 'Sign in with Google' })).toHaveAttribute('href', '/auth/google/login');
    expect(screen.getByRole('link', { name: 'Sign in with GitHub' })).toHaveAttribute('href', '/auth/github/login');
  });

  it('names a provider it does not know by its ID', async () => {
    renderApp(<SignInPage />, { client: client(['okta']) });
    expect(await screen.findByRole('link', { name: 'Sign in with okta' })).toHaveAttribute('href', '/auth/okta/login');
  });

  it('says so when no provider is configured', async () => {
    renderApp(<SignInPage />, { client: client([]) });
    expect(await screen.findByText('No sign-in provider is configured.')).toBeInTheDocument();
  });

  it('says so when the providers cannot be loaded', async () => {
    const broken = fakeClient({ 'GET /auth/providers': () => jsonResponse({ error: { code: 'internal', message: 'x' } }, 500) });
    renderApp(<SignInPage />, { client: broken });
    expect(await screen.findByText('The sign-in options could not be loaded.')).toBeInTheDocument();
  });

  it.each([
    ['not_allowed', "This account isn't allowed to sign in."],
    ['state', 'The sign-in attempt expired or could not be verified. Please try again.'],
    ['denied', 'Sign-in was cancelled.'],
    ['failed', 'Sign-in failed. Please try again.'],
  ])('?signin_error=%s shows its fixed text', async (code, text) => {
    renderApp(<SignInPage />, { client: client(['google']), route: `/?signin_error=${code}` });
    expect(await screen.findByRole('alert')).toHaveTextContent(text);
  });

  it('shows nothing for an unknown error code and never echoes it', async () => {
    renderApp(<SignInPage />, { client: client(['google']), route: '/?signin_error=<b>boom</b>' });
    await screen.findByRole('link', { name: 'Sign in with Google' });
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByText(/boom/)).toBeNull();
  });
});

describe('signinErrorMessage', () => {
  it('maps the four known codes and nothing else', () => {
    expect(signinErrorMessage('not_allowed')).toBe("This account isn't allowed to sign in.");
    expect(signinErrorMessage(null)).toBeUndefined();
    expect(signinErrorMessage('constructor')).toBeUndefined();
  });
});
