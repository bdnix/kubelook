import { screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, renderApp } from '../../test/render';
import { ModuleGate } from './ModuleGate';

const mount = (modules: unknown[]) =>
  renderApp(
    <ModuleGate cluster="c" name="keda" fallback={<p>no keda</p>}>
      <p>keda page</p>
    </ModuleGate>,
    { client: createClient({ fetch: vi.fn(() => Promise.resolve(jsonResponse({ modules }))) }) },
  );

describe('ModuleGate', () => {
  it('shows its children when the module is active', async () => {
    mount([{ name: 'keda', active: true, kinds: [] }]);
    expect(await screen.findByText('keda page')).toBeInTheDocument();
  });

  it('shows the fallback when the module is inactive or unknown', async () => {
    mount([{ name: 'keda', active: false, reason: 'disabled', kinds: [] }]);
    expect(await screen.findByText('no keda')).toBeInTheDocument();
    expect(screen.queryByText('keda page')).toBeNull();
  });

  it('shows nothing while the modules load', () => {
    mount([]);
    expect(screen.queryByText('keda page')).toBeNull();
    expect(screen.queryByText('no keda')).toBeNull();
  });
});
