import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { LiveContext } from '../../state/useLive';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { makeLiveStore } from '../../test/live';
import { jsonResponse, renderApp } from '../../test/render';
import { NamespaceFilter } from './NamespaceFilter';

const settingsDoc = (filters: Record<string, string[]>) => ({
  favouriteClusters: [],
  namespaceFilters: filters,
  columnPresets: {},
  filterSets: [],
  watchIdleSeconds: 30,
  theme: 'system',
});
const ns = (name: string) => ({ metadata: { uid: name, name } });

function mount(saved: Record<string, string[]> = {}) {
  const puts: Array<{ namespaceFilters: Record<string, string[]> }> = [];
  let doc = settingsDoc(saved);
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    if (url.endsWith('/settings')) {
      if (init?.method === 'PUT') {
        doc = JSON.parse(String(init.body)) as typeof doc;
        puts.push(doc);
      }
      return Promise.resolve(jsonResponse(doc));
    }
    return Promise.resolve(jsonResponse({}));
  });
  const store = makeLiveStore();
  renderApp(
    <LiveContext.Provider value={store}>
      <NamespaceFilter cluster="v:prod" />
    </LiveContext.Provider>,
    { client: createClient({ fetch }) },
  );
  /** The namespaces are watched once the list is opened: the server answers then. */
  const serve = async () => {
    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
    act(() => FakeWebSocket.last.serverOpen());
    act(() =>
      FakeWebSocket.last.serverSend({
        ch: 1,
        t: 'snapshot',
        d: { items: ['a', 'b', 'c'].map(ns), more: false },
      }),
    );
  };
  return { puts, serve };
}

describe('NamespaceFilter', () => {
  it('shows "All namespaces" without a selection and lists the namespaces of the cluster', async () => {
    const { serve } = mount();
    const button = await screen.findByRole('button', { name: 'Namespaces: all' });
    expect(FakeWebSocket.instances).toHaveLength(0); // closed: no watch
    await userEvent.click(button);
    await serve();
    const list = screen.getByRole('group', { name: 'Namespaces' });
    expect(
      within(list)
        .getAllByRole('checkbox')
        .map((c) => c.getAttribute('aria-label') ?? c.parentElement?.textContent),
    ).toEqual(['a', 'b', 'c']);
  });

  it('selecting namespaces saves them per cluster', async () => {
    const { puts, serve } = mount();
    await userEvent.click(await screen.findByRole('button', { name: 'Namespaces: all' }));
    await serve();
    await userEvent.click(screen.getByRole('checkbox', { name: 'a' }));
    await userEvent.click(screen.getByRole('checkbox', { name: 'c' }));
    await waitFor(() => expect(puts).toHaveLength(2));
    expect(puts[1]?.namespaceFilters).toEqual({ 'v:prod': ['a', 'c'] });
    expect(await screen.findByRole('button', { name: 'Namespaces: a, c' })).toBeInTheDocument();
  });

  it('shows the saved selection, and clearing it goes back to all namespaces', async () => {
    const { puts, serve } = mount({ 'v:prod': ['b'] });
    const button = await screen.findByRole('button', { name: 'Namespaces: b' });
    await userEvent.click(button);
    await serve();
    await userEvent.click(screen.getByRole('button', { name: 'Clear selection' }));
    await waitFor(() => expect(puts.at(-1)?.namespaceFilters).toEqual({}));
    expect(await screen.findByRole('button', { name: 'Namespaces: all' })).toBeInTheDocument();
  });

  it('a long selection is counted instead of listed', async () => {
    mount({ 'v:prod': ['n1', 'n2', 'n3', 'n4', 'n5'] });
    expect(await screen.findByRole('button', { name: 'Namespaces: 5 selected' })).toBeInTheDocument();
  });
});
