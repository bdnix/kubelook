import { screen } from '@testing-library/react';
import { Route, Routes } from 'react-router';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, renderApp } from '../../test/render';
import { ModulePage } from './ModulePage';

const modules = [
  {
    name: 'keda',
    active: true,
    kinds: [{ group: 'keda.sh', version: 'v1alpha1', resource: 'scaledobjects', kind: 'ScaledObject', namespaced: true }],
  },
  { name: 'trivy', active: false, reason: 'not_served', kinds: [] },
];

function mount(name: string) {
  renderApp(
    <Routes>
      <Route path="/c/:cluster/modules/:name" element={<ModulePage cluster="c" />} />
    </Routes>,
    {
      route: `/c/c/modules/${name}`,
      client: createClient({ fetch: vi.fn(() => Promise.resolve(jsonResponse({ modules }))) }),
    },
  );
}

describe('ModulePage', () => {
  it('shows the module and links to the lists of its kinds', async () => {
    mount('keda');
    expect(await screen.findByRole('heading', { name: 'KEDA' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'ScaledObject' })).toHaveAttribute('href', '/c/c/keda.sh/ScaledObject');
  });

  it('says so when the cluster does not have the module', async () => {
    mount('trivy');
    expect(await screen.findByText('This cluster does not have this module.')).toBeInTheDocument();
  });

  it('says so for a name that is no module', async () => {
    mount('nope');
    expect(await screen.findByText('This cluster does not have this module.')).toBeInTheDocument();
  });
});
