import { describe, expect, it, vi } from 'vitest';
import { createDockStore, logTab, shellTab, type DockTab } from '../../state/dock';
import { createPopout, DOCK_CHANNEL, createPopoutPage, type ChannelLike } from './popout';

/** A pair of channels that deliver to each other, like two windows' BroadcastChannels. */
function channelPair(): [ChannelLike, ChannelLike] {
  const make = (): ChannelLike & {
    peer?: ChannelLike & { deliver(data: unknown): void };
    deliver(data: unknown): void;
  } => {
    const ch = {
      onmessage: null as ((e: { data: unknown }) => void) | null,
      postMessage(data: unknown) {
        ch.peer?.deliver(JSON.parse(JSON.stringify(data)));
      },
      deliver(data: unknown) {
        ch.onmessage?.({ data });
      },
      close: vi.fn(),
      peer: undefined as undefined | { deliver(data: unknown): void },
    };
    return ch as never;
  };
  const a = make() as unknown as ChannelLike & { peer: unknown };
  const b = make() as unknown as ChannelLike & { peer: unknown };
  a.peer = b;
  b.peer = a;
  return [a, b];
}

const log = (pod: string): DockTab =>
  logTab({ cluster: 'v:prod', namespace: 'ns', pod, containers: ['app'] });
const shell = (pod: string): DockTab =>
  shellTab({ cluster: 'v:prod', namespace: 'ns', pod, container: 'app' }, 1);

describe('dock pop-out', () => {
  it('opens /dock in a new window, with the shared channel name', () => {
    expect(DOCK_CHANNEL).toBe('kubelook-dock');
    const main = createDockStore();
    const [mainChannel] = channelPair();
    const openWindow = vi.fn(() => ({ closed: false }) as Window);
    const popout = createPopout({ store: main, openWindow, channel: mainChannel });
    main.getState().open(log('a'));
    expect(popout.popOut([main.getState().tabs[0]?.id as string])).toBe(true);
    expect(openWindow).toHaveBeenCalledWith('/dock', 'kubelook-dock', expect.stringContaining('popup'));
  });

  it('moves the chosen tabs to the pop-out when it says it is ready', () => {
    const main = createDockStore();
    const pop = createDockStore();
    const [mc, pc] = channelPair();
    const popout = createPopout({ store: main, openWindow: () => ({}) as Window, channel: mc });
    createPopoutPage({ store: pop, channel: pc, onPageHide: () => undefined });
    main.getState().open(log('a'));
    main.getState().open(log('b'));
    const ids = main.getState().tabs.map((t) => t.id);
    popout.popOut([ids[0] as string]);
    expect(main.getState().tabs.map((t) => t.id)).toEqual([ids[1]]);
    expect(pop.getState().tabs.map((t) => t.id)).toEqual([ids[0]]);
    expect(pop.getState().active).toBe(ids[0]);
  });

  it('a window that cannot be opened leaves the tabs where they are', () => {
    const main = createDockStore();
    const [mc] = channelPair();
    const popout = createPopout({ store: main, openWindow: () => null, channel: mc });
    main.getState().open(log('a'));
    expect(popout.popOut([main.getState().tabs[0]?.id as string])).toBe(false);
    expect(main.getState().tabs).toHaveLength(1);
  });

  it('a shell cannot move: it arrives marked, so the pop-out asks for a new shell', () => {
    const main = createDockStore();
    const pop = createDockStore();
    const [mc, pc] = channelPair();
    const popout = createPopout({ store: main, openWindow: () => ({}) as Window, channel: mc });
    createPopoutPage({ store: pop, channel: pc, onPageHide: () => undefined });
    main.getState().open(shell('db'));
    popout.popOut([main.getState().tabs[0]?.id as string]);
    const moved = pop.getState().tabs[0];
    expect(moved?.kind).toBe('shell');
    expect((moved?.props as { moved?: boolean }).moved).toBe(true);
  });

  it('closing the pop-out returns its tabs to the main window', () => {
    const main = createDockStore();
    const pop = createDockStore();
    const [mc, pc] = channelPair();
    const popout = createPopout({ store: main, openWindow: () => ({}) as Window, channel: mc });
    let hide: () => void = () => undefined;
    createPopoutPage({ store: pop, channel: pc, onPageHide: (fn) => (hide = fn) });
    main.getState().open(log('a'));
    popout.popOut([main.getState().tabs[0]?.id as string]);
    pop.getState().open(log('c'));
    hide();
    expect(main.getState().tabs.map((t) => (t.props as { pod: string }).pod)).toEqual(['a', 'c']);
  });

  it('tabs that are already open in the main window are not opened twice', () => {
    const main = createDockStore();
    const pop = createDockStore();
    const [mc, pc] = channelPair();
    createPopout({ store: main, openWindow: () => ({}) as Window, channel: mc });
    let hide: () => void = () => undefined;
    createPopoutPage({ store: pop, channel: pc, onPageHide: (fn) => (hide = fn) });
    main.getState().open(log('a'));
    pop.getState().open(log('a'));
    hide();
    expect(main.getState().tabs).toHaveLength(1);
  });
});
