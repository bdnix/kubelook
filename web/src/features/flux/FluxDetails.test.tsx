import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, renderApp } from '../../test/render';
import { FluxDetails } from './FluxDetails';

const ks = (over: Record<string, unknown> = {}) => ({
  metadata: { name: 'apps', namespace: 'flux-system' },
  spec: {},
  status: {
    conditions: [{ type: 'Ready', status: 'True', reason: 'ReconciliationSucceeded', message: 'Applied revision main@sha1:abc', lastTransitionTime: '2026-11-20T09:00:00Z' }],
    lastAppliedRevision: 'main@sha1:abc',
    lastAttemptedRevision: 'main@sha1:def',
  },
  ...over,
});

function mount(object: Record<string, unknown>, kind = 'Kustomization', group = 'kustomize.toolkit.fluxcd.io', version = 'v1', resource = 'kustomizations') {
  const posted: unknown[] = [];
  const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url.includes('/flux/inventory')) {
      return Promise.resolve(jsonResponse({ entries: [{ group: 'apps', version: 'v1', kind: 'Deployment', namespace: 'shop', name: 'web' }, { group: '', version: 'v1', kind: 'Service', namespace: 'shop', name: 'web' }] }));
    }
    if (init?.method === 'POST') posted.push(JSON.parse(String(init.body)));
    return Promise.resolve(jsonResponse({}));
  });
  renderApp(<FluxDetails cluster="c" group={group} version={version} resource={resource} kind={kind} object={object} now={Date.parse('2026-11-20T10:00:00Z')} />, { client: createClient({ fetch }) });
  return { posted, fetch };
}

describe('FluxDetails', () => {
  it('shows the conditions, the revisions and the inventory with links to the views', async () => {
    mount(ks());
    expect(screen.getByRole('table', { name: 'Conditions' })).toHaveTextContent('Applied revision main@sha1:abc');
    expect(screen.getByText('Last applied revision')).toBeInTheDocument();
    expect(screen.getAllByText('main@sha1:abc').length).toBeGreaterThan(0);
    expect(screen.getByText('main@sha1:def')).toBeInTheDocument();
    const inventory = await screen.findByRole('list', { name: 'Inventory' });
    expect(within(inventory).getByRole('link', { name: 'Deployment web' })).toHaveAttribute('href', '/c/c/ns/shop/apps/Deployment/web');
    expect(within(inventory).getByRole('link', { name: 'Service web' })).toHaveAttribute('href', '/c/c/ns/shop/core/Service/web');
  });

  it('shows the source artifact of a source', () => {
    mount({ metadata: { name: 'repo', namespace: 'flux-system' }, status: { artifact: { revision: 'main@sha1:abc', url: 'http://x/y.tgz' }, conditions: [] } }, 'GitRepository', 'source.toolkit.fluxcd.io', 'v1', 'gitrepositories');
    expect(screen.getByText('Artifact revision')).toBeInTheDocument();
    expect(screen.queryByRole('list', { name: 'Inventory' })).toBeNull();
  });

  it('shows the history of a HelmRelease', async () => {
    mount({ metadata: { name: 'web', namespace: 'flux-system' }, status: { conditions: [], history: [{ chartName: 'web', chartVersion: '1.1.0', status: 'deployed', name: 'web', namespace: 'shop', version: 2, digest: 'sha256:aa' }] } }, 'HelmRelease', 'helm.toolkit.fluxcd.io', 'v2', 'helmreleases');
    const table = screen.getByRole('table', { name: 'History' });
    expect(table).toHaveTextContent('1.1.0');
    expect(table).toHaveTextContent('deployed');
  });

  it('says when the inventory is unavailable', async () => {
    const fetch = vi.fn(() => Promise.resolve(jsonResponse({ entries: [], note: 'inventory unavailable' })));
    renderApp(<FluxDetails cluster="c" group="helm.toolkit.fluxcd.io" version="v2" resource="helmreleases" kind="HelmRelease" object={{ metadata: { name: 'web', namespace: 'f' }, status: {} }} now={0} />, { client: createClient({ fetch }) });
    expect(await screen.findByText('inventory unavailable')).toBeInTheDocument();
  });

  it('reconciles, suspends and resumes through the actions endpoint', async () => {
    const { posted } = mount(ks());
    await userEvent.click(screen.getByRole('button', { name: 'Reconcile' }));
    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]).toEqual({ action: 'fluxReconcile', target: { group: 'kustomize.toolkit.fluxcd.io', version: 'v1', resource: 'kustomizations', namespace: 'flux-system', name: 'apps' } });
    await userEvent.click(screen.getByRole('button', { name: 'Suspend' }));
    await waitFor(() => expect(posted).toHaveLength(2));
    expect((posted[1] as { action: string }).action).toBe('fluxSuspend');
    expect(screen.getByRole('button', { name: 'Resume' })).toBeDisabled();
  });

  it('disables Reconcile and Suspend while suspended and gives the reason', async () => {
    const { posted } = mount(ks({ spec: { suspend: true } }));
    const reconcile = screen.getByRole('button', { name: 'Reconcile' });
    expect(reconcile).toBeDisabled();
    expect(reconcile).toHaveAttribute('title', 'Resume the object before reconciling.');
    expect(screen.getByRole('button', { name: 'Suspend' })).toBeDisabled();
    await userEvent.click(screen.getByRole('button', { name: 'Resume' }));
    await waitFor(() => expect(posted).toHaveLength(1));
    expect((posted[0] as { action: string }).action).toBe('fluxResume');
  });

  it('does not offer Suspend for a FluxReport', () => {
    mount({ metadata: { name: 'flux', namespace: 'flux-system' }, status: {} }, 'FluxReport', 'fluxcd.controlplane.io', 'v1', 'fluxreports');
    expect(screen.getByRole('button', { name: 'Reconcile' })).toBeEnabled();
    expect(screen.queryByRole('button', { name: 'Suspend' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Resume' })).toBeNull();
  });

  it('shows the error of an action', async () => {
    const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) =>
      Promise.resolve(init?.method === 'POST' ? jsonResponse({ error: { code: 'forbidden', message: 'Not allowed.' } }, 403) : jsonResponse({ entries: [] })),
    );
    renderApp(<FluxDetails cluster="c" group="kustomize.toolkit.fluxcd.io" version="v1" resource="kustomizations" kind="Kustomization" object={ks()} now={0} />, { client: createClient({ fetch }) });
    await userEvent.click(screen.getByRole('button', { name: 'Reconcile' }));
    expect(await screen.findByText('Not allowed.')).toBeInTheDocument();
  });
});
