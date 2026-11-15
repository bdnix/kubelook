import type { StoreApi } from 'zustand/vanilla';
import type { DockState, DockTab } from '../../state/dock';

/** The name of the BroadcastChannel the main window and the pop-out talk over. */
export const DOCK_CHANNEL = 'kubelook-dock';

/** The part of a BroadcastChannel the dock uses; tests pass a pair of in-memory ones. */
export interface ChannelLike {
  onmessage: ((e: { data: unknown }) => void) | null;
  postMessage(data: unknown): void;
  close(): void;
}

type Message =
  | { type: 'ready' }
  | { type: 'adopt'; tabs: DockTab[] }
  | { type: 'adopted' }
  | { type: 'return'; tabs: DockTab[] };

const FEATURES = 'popup,width=1100,height=700';

export interface PopoutDeps {
  store: StoreApi<DockState>;
  /** `window.open`; returns null when the browser blocks the window. */
  openWindow: (url: string, name: string, features: string) => Window | null;
  channel: ChannelLike;
}

/** The main window's side: moves tabs to a new window and takes them back when that window closes. */
export function createPopout({ store, openWindow, channel }: PopoutDeps) {
  let pending: DockTab[] = [];
  channel.onmessage = (e) => {
    const m = e.data as Message;
    if (m.type === 'ready' && pending.length > 0) {
      // a window that has just loaded asks for what was meant for it
      channel.postMessage({ type: 'adopt', tabs: pending } satisfies Message);
    } else if (m.type === 'adopted') {
      pending = [];
    } else if (m.type === 'return') {
      for (const tab of m.tabs) {
        if (!store.getState().tabs.some((t) => t.id === tab.id)) store.getState().open(tab);
      }
    }
  };
  return {
    /** Moves the tabs to the pop-out; false (and nothing moves) when the window could not be opened. */
    popOut(ids: string[]): boolean {
      const tabs = store.getState().tabs.filter((t) => ids.includes(t.id));
      if (tabs.length === 0) return false;
      if (!openWindow('/dock', DOCK_CHANNEL, FEATURES)) return false;
      pending = tabs;
      for (const t of tabs) store.getState().close(t.id);
      // A window that is open already is listening; a new one asks with "ready" when it has loaded.
      channel.postMessage({ type: 'adopt', tabs } satisfies Message);
      return true;
    },
  };
}

export interface PopoutPageDeps {
  store: StoreApi<DockState>;
  channel: ChannelLike;
  /** Registers the function to call when the pop-out is closed. */
  onPageHide: (fn: () => void) => void;
}

/** The pop-out's side: announces itself, opens the tabs it is given and returns its tabs when it closes. */
export function createPopoutPage({ store, channel, onPageHide }: PopoutPageDeps) {
  channel.onmessage = (e) => {
    const m = e.data as Message;
    if (m.type !== 'adopt') return;
    for (const tab of m.tabs) {
      // A shell is a running process in the other window: it cannot move, so it comes as a prompt for a new one.
      store.getState().open(tab.kind === 'shell' ? { ...tab, props: { ...tab.props, moved: true } } : tab);
    }
    channel.postMessage({ type: 'adopted' } satisfies Message);
  };
  onPageHide(() => {
    channel.postMessage({ type: 'return', tabs: store.getState().tabs } satisfies Message);
  });
  channel.postMessage({ type: 'ready' } satisfies Message);
}

/** The browser's BroadcastChannel of that name, as a ChannelLike. */
export function browserChannel(name: string): ChannelLike {
  const bc = new BroadcastChannel(name);
  let handler: ChannelLike['onmessage'] = null;
  return {
    get onmessage() {
      return handler;
    },
    set onmessage(fn) {
      handler = fn;
      bc.onmessage = fn ? (e: MessageEvent) => fn({ data: e.data as unknown }) : null;
    },
    postMessage: (data) => bc.postMessage(data),
    close: () => bc.close(),
  };
}
