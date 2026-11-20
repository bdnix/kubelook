import { act, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { renderApp } from '../../test/render';
import { helmApi, release } from './fixtures';
import { ReleasesPage } from './ReleasesPage';

vi.mock('../../adapters/monaco/YamlEditor', () => ({
  YamlEditor: (p: { value: string; onChange: (v: string) => void; label: string }) => (
    <textarea aria-label={p.label} value={p.value} onChange={(e) => p.onChange(e.target.value)} />
  ),
}));

function mount(api = helmApi()) {
  renderApp(
    <Routes>
      <Route path="/c/:cluster/helm" element={<ReleasesPage cluster="c" />} />
    </Routes>,
    { client: api.client, route: '/c/c/helm' },
  );
  return api;
}

describe('ReleasesPage', () => {
  it('lists the releases with their columns', async () => {
    mount();
    const table = await screen.findByRole('table', { name: 'Releases' });
    const row = within(table).getByRole('row', { name: /web/ });
    for (const text of ['web', 'team-a', 'deployed', '1.1.0', '2.1', '2']) {
      expect(within(row).getAllByText(text).length).toBeGreaterThan(0);
    }
    expect(within(table).getAllByRole('columnheader').map((h) => h.textContent)).toEqual([
      'Name',
      'Namespace',
      'Status',
      'Chart',
      'App version',
      'Revision',
      'Updated',
    ]);
  });

  it('says the console uses Helm 4 and manages releases made by Helm 3', async () => {
    mount();
    expect(await screen.findByText(/Helm 4/)).toHaveTextContent(/releases made by Helm 3/);
  });

  it('says there are no releases', async () => {
    mount(helmApi({ releases: [] }));
    expect(await screen.findByText('There are no Helm releases in this cluster.')).toBeInTheDocument();
  });

  describe('polling', () => {
    beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }));
    afterEach(() => vi.useRealTimers());

    it('reads the releases again every 15 seconds', async () => {
      const api = mount();
      await screen.findByRole('table', { name: 'Releases' });
      const reads = () => api.calls.filter((c) => c.method === 'GET' && c.path.endsWith('/helm/releases')).length;
      expect(reads()).toBe(1);
      await act(() => vi.advanceTimersByTimeAsync(14_000));
      expect(reads()).toBe(1);
      await act(() => vi.advanceTimersByTimeAsync(2_000));
      expect(reads()).toBe(2);
    });
  });

  it('opens the details of a release', async () => {
    mount();
    await userEvent.click(await screen.findByRole('button', { name: 'web' }));
    expect(await screen.findByRole('region', { name: 'Release web' })).toBeInTheDocument();
  });

  it('shows the release the server lists next to a failed one', async () => {
    mount(helmApi({ releases: [release(), release({ name: 'broken', status: 'failed' })] }));
    expect(await screen.findByText('failed')).toBeInTheDocument();
  });
});
