import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { App } from './App';
import { createClient } from './api/client';
import { jsonResponse } from './test/render';

describe('App', () => {
  it('shows the product name as the main heading when signed out', async () => {
    const client = createClient({
      fetch: () =>
        Promise.resolve(jsonResponse({ error: { code: 'unauthenticated', message: 'Sign in.' } }, 401)),
    });
    render(<App client={client} />);
    expect(await screen.findByRole('heading', { level: 1, name: 'Kubelook' })).toBeInTheDocument();
  });

  it('shows the layout when signed in', async () => {
    const client = createClient({
      fetch: () => Promise.resolve(jsonResponse({ id: 'u1', email: 'a@example.com', name: 'Ann' })),
    });
    render(<App client={client} />);
    expect(await screen.findByRole('navigation', { name: 'Clusters' })).toBeInTheDocument();
  });
});
