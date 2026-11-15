import { screen } from '@testing-library/react';
import { Route, Routes } from 'react-router';
import { describe, expect, it } from 'vitest';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { ClusterPage } from './ClusterPage';
import { NoAccessPage } from '../shell/NoAccessPage';

const clusters = {
  sources: [
    {
      type: 'vault',
      label: 'Vault',
      status: 'ok',
      clusters: [{ id: 'v:prod', name: 'prod', state: 'closed' }],
    },
  ],
};

function mount(route: string, extra: Record<string, () => Response> = {}) {
  return renderApp(
    <Routes>
      <Route path="/c/:cluster/*" element={<ClusterPage />} />
    </Routes>,
    { route, client: fakeClient({ 'GET /clusters': () => jsonResponse(clusters), ...extra }) },
  );
}

describe('deep links', () => {
  it('a cluster that is not in the session says how to get it', async () => {
    mount('/c/k%3Aprod.yaml%2Fctx/core/Pod');
    expect(
      await screen.findByText(
        "This cluster isn't available in your session. If it comes from a kubeconfig, upload that kubeconfig first.",
      ),
    ).toBeInTheDocument();
  });

  it('a Vault cluster the token cannot reach says the user has no access', async () => {
    mount('/c/v%3Aprod/core/Pod', {
      'POST /clusters/v%3Aprod/open': () =>
        jsonResponse({ error: { code: 'forbidden', message: 'permission denied' } }, 403),
    });
    expect(await screen.findByText("You don't have access to this cluster.")).toBeInTheDocument();
  });

  it('a cluster in the session opens as usual', async () => {
    mount('/c/v%3Aprod/core/Pod', { 'POST /clusters/v%3Aprod/open': () => jsonResponse({ state: 'open' }) });
    expect(screen.queryByText(/isn't available/)).toBeNull();
    expect(await screen.findByText(/Opening the cluster|Loading/)).toBeInTheDocument();
  });
});

describe('NoAccessPage', () => {
  it('has a message for each reason', () => {
    renderApp(
      <>
        <NoAccessPage reason="unknown" />
        <NoAccessPage reason="forbidden" />
      </>,
    );
    expect(screen.getAllByRole('heading')).toHaveLength(2);
    expect(screen.getByText("You don't have access to this cluster.")).toBeInTheDocument();
  });
});
