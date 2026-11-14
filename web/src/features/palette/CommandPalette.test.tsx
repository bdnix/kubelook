import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes, useLocation } from 'react-router';
import { describe, expect, it } from 'vitest';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { CommandPalette } from './CommandPalette';
import { fuzzyScore } from './usePaletteItems';

const res = (resource: string, kind: string, group = '') => ({
  group,
  version: 'v1',
  resource,
  kind,
  namespaced: true,
  verbs: ['list'],
  preferred: true,
  servedVersions: ['v1'],
});
const discovery = {
  serverVersion: 'v1.35.0',
  resources: [
    res('pods', 'Pod'),
    res('deployments', 'Deployment', 'apps'),
    res('services', 'Service'),
    res('nodes', 'Node'),
  ],
};
const client = fakeClient({ 'GET /clusters/v%3Aprod/discovery': () => jsonResponse(discovery) });

function Where() {
  return <p data-testid="where">{decodeURIComponent(useLocation().pathname)}</p>;
}

function mount(route = '/c/v%3Aprod/core/Pod') {
  return renderApp(
    <>
      <CommandPalette />
      <Routes>
        <Route path="*" element={<Where />} />
      </Routes>
    </>,
    { route, client },
  );
}

describe('fuzzyScore', () => {
  it('matches a subsequence, case-insensitively, and prefers early and tight matches', () => {
    expect(fuzzyScore('dpl', 'Deployments')).toBeGreaterThan(0);
    expect(fuzzyScore('xyz', 'Deployments')).toBe(-1);
    expect(fuzzyScore('', 'Anything')).toBe(0);
    expect(fuzzyScore('pod', 'Pods')).toBeGreaterThan(fuzzyScore('pod', 'Persistent Volumes Of Data'));
    expect(fuzzyScore('node', 'Nodes')).toBeGreaterThan(
      fuzzyScore('node', 'Network Policies Of Defaults Everywhere'),
    );
  });
});

describe('CommandPalette', () => {
  it('opens with Ctrl+K and with Cmd+K, as a combobox with a listbox', async () => {
    mount();
    expect(screen.queryByRole('dialog')).toBeNull();
    await userEvent.keyboard('{Control>}k{/Control}');
    const dialog = await screen.findByRole('dialog', { name: 'Command palette' });
    const input = within(dialog).getByRole('combobox');
    expect(input).toHaveAttribute('aria-expanded', 'true');
    expect(input).toHaveAttribute('aria-controls', within(dialog).getByRole('listbox').id);
    expect(input).toHaveFocus();
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).toBeNull();
    await userEvent.keyboard('{Meta>}k{/Meta}');
    expect(await screen.findByRole('dialog', { name: 'Command palette' })).toBeInTheDocument();
  });

  it('lists the kinds the open cluster serves and its views, nothing from other clusters', async () => {
    mount();
    await userEvent.keyboard('{Control>}k{/Control}');
    await waitFor(() => expect(screen.getAllByRole('option').length).toBeGreaterThan(3));
    const titles = screen.getAllByRole('option').map((o) => o.textContent);
    expect(titles).toEqual(
      expect.arrayContaining(['Pods', 'Deployments', 'Services', 'Nodes', 'Pod Files', 'Who can…']),
    );
    expect(titles).not.toContain('ConfigMaps'); // not served by this cluster
  });

  it('without an open cluster there are no kinds, only the settings', async () => {
    mount('/settings');
    await userEvent.keyboard('{Control>}k{/Control}');
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Settings']);
  });

  it('matches the title fuzzily, best first, and Enter goes there', async () => {
    mount();
    await userEvent.keyboard('{Control>}k{/Control}');
    await waitFor(() => expect(screen.getAllByRole('option').length).toBeGreaterThan(3));
    await userEvent.keyboard('dpl');
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Deployments']);
    await userEvent.keyboard('{Enter}');
    expect(screen.getByTestId('where')).toHaveTextContent('/c/v:prod/apps/Deployment');
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('arrow keys move the highlight, which the combobox announces', async () => {
    mount();
    await userEvent.keyboard('{Control>}k{/Control}');
    await waitFor(() => expect(screen.getAllByRole('option').length).toBeGreaterThan(3));
    const input = screen.getByRole('combobox');
    const options = screen.getAllByRole('option');
    expect(options[0]).toHaveAttribute('aria-selected', 'true');
    expect(input).toHaveAttribute('aria-activedescendant', options[0]?.id);
    await userEvent.keyboard('{ArrowDown}');
    expect(screen.getAllByRole('option')[1]).toHaveAttribute('aria-selected', 'true');
    await userEvent.keyboard('{ArrowUp}{ArrowUp}');
    expect(screen.getAllByRole('option').at(-1)).toHaveAttribute('aria-selected', 'true');
  });

  it('says so when nothing matches, and clicking an item goes there', async () => {
    mount();
    await userEvent.keyboard('{Control>}k{/Control}');
    await waitFor(() => expect(screen.getAllByRole('option').length).toBeGreaterThan(3));
    await userEvent.keyboard('zzzz');
    expect(screen.queryAllByRole('option')).toHaveLength(0);
    expect(screen.getByText('Nothing matches.')).toBeInTheDocument();
    await userEvent.clear(screen.getByRole('combobox'));
    await userEvent.click(screen.getByRole('option', { name: 'Nodes' }));
    expect(screen.getByTestId('where')).toHaveTextContent('/c/v:prod/core/Node');
  });
});
