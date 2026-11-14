import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { App } from './App';
import { createClient } from './api/client';
import { jsonResponse } from './test/render';

describe('App', () => {
  it('shows the product name as the main heading when signed out', async () => {
    const client = createClient({
      fetch: (input) =>
        Promise.resolve(
          String(input).endsWith('/auth/providers')
            ? jsonResponse({ providers: [] })
            : jsonResponse({ error: { code: 'unauthenticated', message: 'Sign in.' } }, 401),
        ),
    });
    render(<App client={client} />);
    expect(await screen.findByRole('heading', { level: 1, name: 'Kubelook' })).toBeInTheDocument();
  });

  it('shows the layout when signed in', async () => {
    const client = createClient({
      fetch: (input) =>
        Promise.resolve(
          String(input).endsWith('/clusters')
            ? jsonResponse({ sources: [] })
            : jsonResponse({
                user: { id: 'u1', provider: 'google', email: 'a@example.com', name: 'Ann' },
                csrfToken: 'c',
                settings: { favouriteClusters: null, watchIdleSeconds: 30, theme: 'system' },
                maintenanceBanner: '',
                features: { kubeconfig: true, fileBrowser: true, nodeShell: false, vault: true, helmVersion: 4, modules: {} },
                limits: { fileTransferBytes: 1, logsHistoryMaxLines: 2, logsLiveBufferLines: 3, vaultExpiryWarningSeconds: 1800 },
                vault: null,
                keyId: 'k',
              }),
        ),
    });
    render(<App client={client} />);
    expect(await screen.findByRole('navigation', { name: 'Clusters' })).toBeInTheDocument();
  });
});
