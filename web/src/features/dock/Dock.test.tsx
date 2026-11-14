import { act, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it } from 'vitest';
import { dockStore, logTab } from '../../state/dock';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { SocketContext } from '../../ws/context';
import { createSocket, type WebSocketCtor } from '../../ws/socket';
import { Layout } from '../shell/Layout';
import { Dock } from './Dock';

function mount() {
  FakeWebSocket.reset();
  const socket = createSocket({
    url: 'ws://x/api/v1/ws',
    WebSocketCtor: FakeWebSocket as unknown as WebSocketCtor,
    timers: { setTimeout: () => 0, clearTimeout: () => undefined },
    visibility: { isHidden: () => false, subscribe: () => () => undefined },
  });
  return render(
    <SocketContext.Provider value={socket}>
      <Layout dock={<Dock liveBufferLines={100} />}>
        <p>main</p>
      </Layout>
    </SocketContext.Provider>,
  );
}

const tab = (pod: string) => logTab({ cluster: 'v:prod', namespace: 'ns', pod, containers: ['app'] });

describe('Dock', () => {
  beforeEach(() => {
    dockStore.setState({ tabs: [], active: null });
  });

  it('shows no tabs without open items', () => {
    mount();
    expect(screen.queryByRole('tablist')).toBeNull();
  });

  it('is a region named Dock with keyboard-reachable tabs', async () => {
    mount();
    act(() => {
      dockStore.getState().open(tab('a'));
      dockStore.getState().open(tab('b'));
    });
    const region = screen.getByRole('region', { name: 'Dock' });
    const tabs = within(region).getAllByRole('tab');
    expect(tabs.map((t) => t.textContent)).toEqual(['Logs: a', 'Logs: b']);
    expect(tabs[1]).toHaveAttribute('aria-selected', 'true');
    await userEvent.tab();
    expect(tabs[0]).toHaveFocus();
    await userEvent.keyboard('{Enter}');
    expect(tabs[0]).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tabpanel', { name: 'Logs: a' })).toBeVisible();
    const panels = screen.getAllByRole('tabpanel', { hidden: true });
    expect(panels).toHaveLength(2);
    expect(panels[1]).toHaveAttribute('hidden');
  });

  it('every tab keeps its stream open; closing a tab closes its channel', async () => {
    mount();
    act(() => {
      dockStore.getState().open(tab('a'));
      dockStore.getState().open(tab('b'));
    });
    act(() => FakeWebSocket.last.serverOpen());
    const opens = FakeWebSocket.last.frames.filter((f) => f.op === 'logs.open');
    expect(opens.map((f) => (f.d as { pod: string }).pod)).toEqual(['a', 'b']);

    await userEvent.click(screen.getByRole('button', { name: 'Close Logs: a' }));
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'logs.close')).toEqual([
      { op: 'logs.close', ch: 1 },
    ]);
    expect(screen.queryByRole('tab', { name: 'Logs: a' })).toBeNull();
  });

  it('tabs survive when the component is unmounted and mounted again (the state lives in the store)', () => {
    const first = mount();
    act(() => dockStore.getState().open(tab('a')));
    first.unmount();
    mount();
    expect(screen.getByRole('tab', { name: 'Logs: a' })).toBeInTheDocument();
  });
});
