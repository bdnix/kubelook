import 'fake-indexeddb/auto';
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { IDBFactory } from 'fake-indexeddb';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import type { Me } from '../../api/me';
import { dockStore, logTab, shellTab } from '../../state/dock';
import { themeStore } from '../../state/theme';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { jsonResponse, renderApp } from '../../test/render';
import { SocketContext } from '../../ws/context';
import { createSocket, type WebSocketCtor } from '../../ws/socket';
import { openVaultStore } from '../credentials/vaultStore';
import { SettingsPage } from './SettingsPage';

const settings = {
  favouriteClusters: [],
  namespaceFilters: {},
  columnPresets: {},
  filterSets: [],
  watchIdleSeconds: 30,
  theme: 'system',
};
const me = {
  features: {
    kubeconfig: true,
    vault: true,
    helmVersion: 4,
    fileBrowser: true,
    nodeShell: false,
    modules: {},
  },
} as unknown as Me;

interface Sent {
  method: string;
  url: string;
  body?: Record<string, unknown>;
}

function mount() {
  const sent: Sent[] = [];
  let server = { ...settings };
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET';
    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : undefined;
    if (url.endsWith('/settings')) {
      if (method === 'PUT') {
        sent.push({ method, url, body });
        server = body as typeof server;
      }
      return Promise.resolve(jsonResponse(server));
    }
    if (url.endsWith('/credentials/vault') && method === 'POST') {
      sent.push({ method, url, body });
      const vault = {
        displayName: 'ada',
        entityId: 'e',
        expiresAt: '2026-11-14T13:00:00Z',
        ttlSeconds: 3600,
        renewable: true,
        canRenew: true,
        neverExpires: false,
        warning: false,
        expired: false,
      };
      return Promise.resolve(jsonResponse({ sealed: 'v1.sealed.x.y', vault }));
    }
    return Promise.resolve(jsonResponse({}));
  });
  FakeWebSocket.reset();
  const socket = createSocket({
    url: 'ws://x/api/v1/ws',
    WebSocketCtor: FakeWebSocket as unknown as WebSocketCtor,
    timers: { setTimeout: () => 0, clearTimeout: () => undefined },
    visibility: { isHidden: () => false, subscribe: () => () => undefined },
  });
  // an open channel stands for a running terminal or log stream
  socket.open('logs.open', { cluster: 'v:prod' }, { onFrame: () => undefined });
  renderApp(
    <SocketContext.Provider value={socket}>
      <SettingsPage me={me} />
    </SocketContext.Provider>,
    { client: createClient({ fetch }), vaultStore: openVaultStore('u1') },
  );
  act(() => FakeWebSocket.last.serverOpen());
  return { sent };
}

beforeEach(() => {
  globalThis.indexedDB = new IDBFactory();
  dockStore.setState({ tabs: [], active: null });
  themeStore.getState().setTheme('system');
});

describe('SettingsPage', () => {
  it('has sections for appearance, live updates, columns, the Vault token and saved credentials, and no admin settings', async () => {
    mount();
    await screen.findByRole('region', { name: 'Appearance' });
    for (const name of [
      'Appearance',
      'Live updates',
      'Columns',
      'Replace the Vault token',
      'Saved credentials',
    ]) {
      expect(screen.getByRole('region', { name })).toBeInTheDocument();
    }
    expect(screen.queryByRole('heading', { name: /admin|permission|policy/i })).toBeNull();
  });

  it('the theme selector applies the theme and saves it', async () => {
    const { sent } = mount();
    const select = await screen.findByLabelText('Theme');
    await userEvent.selectOptions(select, 'dark');
    expect(themeStore.getState().theme).toBe('dark');
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]?.body).toMatchObject({ theme: 'dark', watchIdleSeconds: 30 });
  });

  it('the idle timeout is a slider from 30 seconds to 5 minutes', async () => {
    const { sent } = mount();
    const slider = await screen.findByRole('slider', { name: 'Pause hidden live lists after' });
    expect(slider).toHaveAttribute('min', '30');
    expect(slider).toHaveAttribute('max', '300');
    expect(screen.getByText('30 seconds')).toBeInTheDocument();
    fireEvent.change(slider, { target: { value: '120' } });
    expect(await screen.findByText('2 minutes')).toBeInTheDocument();
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]?.body).toMatchObject({ watchIdleSeconds: 120 });
    fireEvent.change(slider, { target: { value: '300' } });
    expect(await screen.findByText('5 minutes')).toBeInTheDocument();
  });

  it('the column chooser saves the visible columns of a kind in order', async () => {
    const { sent } = mount();
    const section = await screen.findByRole('region', { name: 'Columns' });
    await userEvent.selectOptions(within(section).getByLabelText('Kind'), 'Pods');
    await userEvent.click(within(section).getByRole('checkbox', { name: 'Restarts' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    const preset = (sent[0]?.body as { columnPresets: Record<string, string[]> }).columnPresets['/Pod'];
    expect(preset).toBeDefined();
    expect(preset).not.toContain('restarts');
    expect(preset?.[0]).toBe('name');
    await userEvent.click(within(section).getByRole('button', { name: 'Move Status up' }));
    await waitFor(() => expect(sent).toHaveLength(2));
    const moved = (sent[1]?.body as { columnPresets: Record<string, string[]> }).columnPresets['/Pod'] ?? [];
    expect(moved.indexOf('status')).toBe((preset ?? []).indexOf('status') - 1);
    await userEvent.click(within(section).getByRole('button', { name: 'Reset columns' }));
    await waitFor(() => expect(sent).toHaveLength(3));
    expect((sent[2]?.body as { columnPresets: Record<string, string[]> }).columnPresets).toEqual({});
  });

  it('pasting a new Vault token keeps the terminals, log streams and editors and the socket as they are', async () => {
    const { sent } = mount();
    dockStore
      .getState()
      .open(shellTab({ cluster: 'v:prod', namespace: 'ns', pod: 'web-1', container: 'app' }, 1));
    dockStore
      .getState()
      .open(logTab({ cluster: 'v:prod', namespace: 'ns', pod: 'web-1', containers: ['app'] }));
    const before = FakeWebSocket.instances.length;
    const box = await screen.findByRole('region', { name: 'Replace the Vault token' });
    await userEvent.type(within(box).getByLabelText('Paste a new Vault token'), 'hvs.NEW');
    await userEvent.click(within(box).getByRole('button', { name: 'Replace Vault token' }));
    await waitFor(() => expect(sent.some((s) => s.url.endsWith('/credentials/vault'))).toBe(true));
    expect(dockStore.getState().tabs).toHaveLength(2);
    expect(FakeWebSocket.instances).toHaveLength(before);
    expect(FakeWebSocket.last.readyState).toBe(FakeWebSocket.OPEN);
    expect(FakeWebSocket.last.frames.filter((f) => String(f.op).endsWith('.close'))).toEqual([]);
    await waitFor(() => expect(within(box).getByLabelText('Paste a new Vault token')).toHaveValue(''));
  });
});
