import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, renderApp } from '../../test/render';
import { ConsumersPanel } from './ConsumersPanel';
import { SecretOperatorBanner } from './SecretOperatorBanner';
import { VaultSecretDetails } from './VaultSecretDetails';

const vs = (over: Record<string, unknown> = {}) => ({
  metadata: { name: 'db', namespace: 'ns', generation: 3 },
  spec: { path: 'kv/db', secretEngine: 'kv' },
  status: { conditions: [{ type: 'SecretCreated', status: 'False', reason: 'FetchFailed', message: 'permission denied', lastTransitionTime: '2026-11-20T09:00:00Z', observedGeneration: 2 }] },
  ...over,
});
const now = Date.parse('2026-11-20T10:00:00Z');

describe('VaultSecretDetails', () => {
  it('shows the condition with its reason, message and last transition, and the stale badge', () => {
    renderApp(<VaultSecretDetails cluster="c" object={vs()} consumers={[]} now={now} />);
    expect(screen.getByText('SecretCreated')).toBeInTheDocument();
    expect(screen.getByText('FetchFailed')).toBeInTheDocument();
    expect(screen.getByText('permission denied')).toBeInTheDocument();
    expect(screen.getByText('stale')).toBeInTheDocument();
  });

  it('shows no stale badge when the condition is up to date, and links to the generated Secret', () => {
    renderApp(<VaultSecretDetails cluster="c" object={vs({ metadata: { name: 'db', namespace: 'ns', generation: 2 } })} consumers={[]} now={now} />);
    expect(screen.queryByText('stale')).toBeNull();
    expect(screen.getByRole('link', { name: 'Secret db' })).toHaveAttribute('href', '/c/c/ns/ns/core/Secret/db');
  });
});

describe('ConsumersPanel', () => {
  const consumers = [
    { group: 'apps', version: 'v1', resource: 'deployments', kind: 'Deployment', namespace: 'ns', name: 'web' },
    { group: 'apps', version: 'v1', resource: 'statefulsets', kind: 'StatefulSet', namespace: 'ns', name: 'db' },
  ];
  it('lists the consumers and restarts each after a confirmation', async () => {
    const posted: unknown[] = [];
    const fetch = vi.fn((_u: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') posted.push(JSON.parse(String(init.body)));
      return Promise.resolve(jsonResponse({}));
    });
    renderApp(<ConsumersPanel cluster="c" consumers={consumers} />, { client: createClient({ fetch }) });
    expect(screen.getByText('Deployment web')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Restart consumers' }));
    expect(posted).toHaveLength(0);
    const dialog = await screen.findByRole('dialog', { name: 'Restart 2 workloads' });
    expect(dialog).toHaveTextContent('Deployment web');
    await userEvent.click(screen.getByRole('button', { name: 'Restart' }));
    await waitFor(() => expect(posted).toHaveLength(2));
    expect(posted[0]).toEqual({ action: 'restart', target: { group: 'apps', version: 'v1', resource: 'deployments', namespace: 'ns', name: 'web' } });
    expect(posted[1]).toMatchObject({ action: 'restart', target: { resource: 'statefulsets', name: 'db' } });
  });

  it('says so when nothing uses the secret', () => {
    renderApp(<ConsumersPanel cluster="c" consumers={[]} />);
    expect(screen.getByText('No Deployment, StatefulSet or DaemonSet uses this Secret.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Restart consumers' })).toBeNull();
  });

  it('shows the error of a failed restart', async () => {
    const fetch = vi.fn(() => Promise.resolve(jsonResponse({ error: { code: 'forbidden', message: 'Not allowed.' } }, 403)));
    renderApp(<ConsumersPanel cluster="c" consumers={consumers.slice(0, 1)} />, { client: createClient({ fetch }) });
    await userEvent.click(screen.getByRole('button', { name: 'Restart consumers' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Restart' }));
    expect(await screen.findByText('Not allowed.')).toBeInTheDocument();
  });
});

describe('SecretOperatorBanner', () => {
  it('warns about a Secret the operator made and links back to its VaultSecret', () => {
    renderApp(<SecretOperatorBanner cluster="c" secret={{ metadata: { name: 'db', namespace: 'ns', labels: { 'created-by': 'vault-secrets-operator' } } }} />);
    expect(screen.getByText('Edits will be overwritten by the operator.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'VaultSecret db' })).toHaveAttribute('href', '/c/c/ns/ns/ricoberger.de/VaultSecret/db');
  });
  it('renders nothing for any other Secret', () => {
    const { container } = renderApp(<SecretOperatorBanner cluster="c" secret={{ metadata: { name: 'x', namespace: 'ns' } }} />);
    expect(container).toBeEmptyDOMElement();
  });
});
