import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { AppRoutes } from './routes';

const signedIn = fakeClient({
  'GET /me': () => jsonResponse({ id: 'u1', email: 'a@example.com', name: 'Ann' }),
});
const signedOut = fakeClient({
  'GET /me': () => jsonResponse({ error: { code: 'unauthenticated', message: 'Sign in.' } }, 401),
});

describe('AppRoutes (signed in)', () => {
  it('/ shows the cluster picker placeholder', async () => {
    renderApp(<AppRoutes />, { client: signedIn });
    expect(await screen.findByRole('heading', { name: 'Choose a cluster' })).toBeInTheDocument();
  });

  it('/settings shows the settings placeholder', async () => {
    renderApp(<AppRoutes />, { client: signedIn, route: '/settings' });
    expect(await screen.findByRole('heading', { name: 'Settings' })).toBeInTheDocument();
  });

  it('/c/:cluster/* shows the decoded cluster ID', async () => {
    renderApp(<AppRoutes />, { client: signedIn, route: '/c/k%3Aprod%2Fctx/ns/default' });
    expect(await screen.findByText('k:prod/ctx')).toBeInTheDocument();
  });

  it('unknown paths show "Page not found"', async () => {
    renderApp(<AppRoutes />, { client: signedIn, route: '/nope/never' });
    expect(await screen.findByText('Page not found')).toBeInTheDocument();
  });

  it('wraps the pages in the layout', async () => {
    renderApp(<AppRoutes />, { client: signedIn });
    expect(await screen.findByRole('navigation', { name: 'Clusters' })).toBeInTheDocument();
  });
});

describe('AppRoutes (signed out)', () => {
  it.each(['/', '/settings', '/c/v%3Aprod', '/nope'])('%s shows the sign-in placeholder', async (route) => {
    renderApp(<AppRoutes />, { client: signedOut, route });
    expect(await screen.findByRole('heading', { level: 1, name: 'Kubelook' })).toBeInTheDocument();
    expect(screen.getByText('Sign in to continue.')).toBeInTheDocument();
    expect(screen.queryByRole('navigation', { name: 'Clusters' })).toBeNull();
  });
});

describe('AppRoutes (loading and failure)', () => {
  it('shows a loading status first', () => {
    renderApp(<AppRoutes />, { client: signedIn });
    expect(screen.getByRole('status')).toHaveTextContent('Loading');
  });

  it('shows an error banner when the server cannot be reached', async () => {
    const broken = fakeClient({
      'GET /me': () => jsonResponse({ error: { code: 'internal', message: 'x' } }, 500),
    });
    renderApp(<AppRoutes />, { client: broken });
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not reach the server');
  });
});
