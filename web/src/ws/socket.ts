// One WebSocket per tab with tagged channels (see internal/ws). It reconnects with back-off and
// re-opens the channels that were open, so views never have to care about the connection.

export interface WebSocketLike {
  readyState: number;
  onopen: (() => void) | null;
  onmessage: ((ev: { data: string }) => void) | null;
  onclose: ((ev: { code: number }) => void) | null;
  onerror: (() => void) | null;
  send(data: string): void;
  close(code?: number): void;
}

export type WebSocketCtor = new (url: string) => WebSocketLike;

export interface Timers {
  setTimeout(fn: () => void, ms: number): unknown;
  clearTimeout(id: unknown): void;
}

export interface Visibility {
  isHidden(): boolean;
  /** Calls `fn` when the tab's visibility changes; returns the unsubscribe function. */
  subscribe(fn: () => void): () => void;
}

export interface SocketDeps {
  url: string;
  WebSocketCtor: WebSocketCtor;
  timers: Timers;
  visibility: Visibility;
}

/** What a channel hears: every frame the server sends for it (`opened`, `snapshot`, `error`, …). */
export interface ChannelHandlers {
  onFrame(type: string, data: unknown): void;
}

export interface Channel {
  /** Sends an op to the channel, e.g. `echo.say`. */
  send(op: string, data?: unknown): void;
  /** Closes the channel; no more frames arrive. */
  close(): void;
}

export interface Socket {
  /** `data` may be a function, called at every (re)open, so a reopened channel can resume where it stopped. */
  open(op: string, data: unknown | (() => unknown), handlers: ChannelHandlers): Channel;
  /** Registers a callback for the end of the session (close code 4401); returns the unsubscribe function. */
  onSessionEnded(fn: () => void): () => void;
  /** Closes the connection for good. */
  dispose(): void;
}

const SESSION_ENDED = 4401;
const FIRST_DELAY_MS = 1000;
const MAX_DELAY_MS = 30_000;

interface ChannelRecord {
  ch: number;
  op: string;
  data: unknown | (() => unknown);
  handlers: ChannelHandlers;
  paused: boolean;
}

/** The close op that belongs to an open op: `watch.open` → `watch.close`. */
function closeOp(openOp: string): string {
  return openOp.replace(/\.open$/, '.close');
}

export function createSocket(deps: SocketDeps): Socket {
  let ws: WebSocketLike | null = null;
  let nextCh = 1;
  let delay = FIRST_DELAY_MS;
  let retry: unknown = null;
  let disposed = false;
  let ended = false;
  const records = new Map<number, ChannelRecord>();
  const endedListeners = new Set<() => void>();

  const rawSend = (op: string, ch: number, d?: unknown) => {
    const frame = JSON.stringify(d === undefined ? { op, ch } : { op, ch, d });
    if (ws && ws.readyState === 1) ws.send(frame);
  };

  const sendOpen = (r: ChannelRecord) =>
    rawSend(r.op, r.ch, typeof r.data === 'function' ? (r.data as () => unknown)() : r.data);

  const connect = () => {
    if (disposed || ended || ws) return;
    const socket = new deps.WebSocketCtor(deps.url);
    ws = socket;
    socket.onopen = () => {
      delay = FIRST_DELAY_MS;
      rawSend('visibility', 0, { hidden: deps.visibility.isHidden() });
      for (const r of records.values()) {
        r.paused = false; // a fresh connection starts from a fresh snapshot
        sendOpen(r);
      }
    };
    socket.onmessage = (ev) => handle(ev.data);
    socket.onerror = () => undefined; // the close event follows
    socket.onclose = (ev) => {
      if (ws !== socket) return;
      ws = null;
      if (disposed) return;
      if (ev.code === SESSION_ENDED) {
        ended = true;
        endedListeners.forEach((fn) => fn());
        return;
      }
      retry = deps.timers.setTimeout(() => {
        retry = null;
        connect();
      }, delay);
      delay = Math.min(delay * 2, MAX_DELAY_MS);
    };
  };

  const handle = (data: string) => {
    let frame: { ch: number; t: string; d?: unknown };
    try {
      frame = JSON.parse(data) as typeof frame;
    } catch {
      return;
    }
    if (frame.ch === 0) {
      if (frame.t === 'ping') rawSend('pong', 0);
      return;
    }
    const r = records.get(frame.ch);
    if (!r) return;
    if (frame.t === 'paused') r.paused = true;
    if (frame.t === 'closed') records.delete(frame.ch);
    r.handlers.onFrame(frame.t, frame.d);
  };

  // Visibility: tell the server, and bring back what it paused while the tab was hidden.
  deps.visibility.subscribe(() => {
    const hidden = deps.visibility.isHidden();
    rawSend('visibility', 0, { hidden });
    if (hidden) return;
    for (const r of [...records.values()]) {
      if (!r.paused) continue;
      rawSend(closeOp(r.op), r.ch);
      records.delete(r.ch);
      r.ch = nextCh++;
      r.paused = false;
      records.set(r.ch, r);
      sendOpen(r);
    }
  });

  return {
    open(op, data, handlers) {
      const r: ChannelRecord = { ch: nextCh++, op, data, handlers, paused: false };
      records.set(r.ch, r);
      if (ws === null) connect();
      else sendOpen(r);
      return {
        send: (o, d) => rawSend(o, r.ch, d),
        close: () => {
          if (!records.has(r.ch)) return;
          rawSend(closeOp(r.op), r.ch);
          records.delete(r.ch);
        },
      };
    },
    onSessionEnded(fn) {
      endedListeners.add(fn);
      return () => endedListeners.delete(fn);
    },
    dispose() {
      disposed = true;
      if (retry !== null) deps.timers.clearTimeout(retry);
      ws?.close();
      ws = null;
      records.clear();
    },
  };
}

/** The WebSocket URL of this page's server. */
export function socketUrl(loc: { protocol: string; host: string }): string {
  return `${loc.protocol === 'https:' ? 'wss' : 'ws'}://${loc.host}/api/v1/ws`;
}

/** The socket the running app uses. */
export function browserSocket(): Socket {
  return createSocket({
    url: socketUrl(window.location),
    WebSocketCtor: WebSocket as unknown as WebSocketCtor,
    timers: { setTimeout: (fn, ms) => setTimeout(fn, ms), clearTimeout: (id) => clearTimeout(id as number) },
    visibility: {
      isHidden: () => document.visibilityState === 'hidden',
      subscribe: (fn) => {
        document.addEventListener('visibilitychange', fn);
        return () => document.removeEventListener('visibilitychange', fn);
      },
    },
  });
}
