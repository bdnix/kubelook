import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FakeWebSocket } from '../test/FakeWebSocket';
import { createSocket, socketUrl, type Socket, type WebSocketCtor } from './socket';

type Frame = { type: string; data: unknown };

function setup() {
  FakeWebSocket.reset();
  let hidden = false;
  let notify: () => void = () => undefined;
  const timers = {
    setTimeout: vi.fn((fn: () => void, ms: number) => globalThis.setTimeout(fn, ms)),
    clearTimeout: vi.fn((id: unknown) => globalThis.clearTimeout(id as number)),
  };
  const socket = createSocket({
    url: 'ws://app/api/v1/ws',
    WebSocketCtor: FakeWebSocket as unknown as WebSocketCtor,
    timers,
    visibility: {
      isHidden: () => hidden,
      subscribe: (fn) => {
        notify = fn;
        return () => undefined;
      },
    },
  });
  return {
    socket,
    timers,
    setHidden(h: boolean) {
      hidden = h;
      notify();
    },
  };
}

function collect(socket: Socket, op = 'watch.open', data: unknown = { resource: 'pods' }) {
  const frames: Frame[] = [];
  const channel = socket.open(op, data, { onFrame: (type, d) => frames.push({ type, data: d }) });
  return { channel, frames };
}

describe('createSocket', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('a channel that cannot be resumed is dropped when the connection is lost, not re-opened', () => {
    const { socket } = setup();
    const lost = vi.fn();
    const resumable = collect(socket);
    socket.open('exec.open', { pod: 'p' }, { onFrame: () => undefined, onConnectionLost: lost });
    FakeWebSocket.last.serverOpen();
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'exec.open')).toHaveLength(1);
    FakeWebSocket.last.close(1006);
    expect(lost).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(1000);
    FakeWebSocket.last.serverOpen();
    const reopened = FakeWebSocket.last.frames.map((f) => f.op);
    expect(reopened).toContain('watch.open');
    expect(reopened).not.toContain('exec.open');
    expect(resumable.frames).toEqual([]);
  });

  it('connects on the first open, then sends the open frame with increasing channel numbers', () => {
    const { socket } = setup();
    expect(FakeWebSocket.instances).toHaveLength(0);
    collect(socket);
    expect(FakeWebSocket.instances).toHaveLength(1);
    FakeWebSocket.last.serverOpen();
    collect(socket, 'logs.open', { pod: 'a' });
    const frames = FakeWebSocket.last.frames;
    expect(frames[0]).toEqual({ op: 'visibility', ch: 0, d: { hidden: false } });
    expect(frames[1]).toEqual({ op: 'watch.open', ch: 1, d: { resource: 'pods' } });
    expect(frames[2]).toEqual({ op: 'logs.open', ch: 2, d: { pod: 'a' } });
  });

  it('routes frames to the right handlers and ignores unknown channels and bad JSON', () => {
    const { socket } = setup();
    const a = collect(socket);
    const b = collect(socket);
    FakeWebSocket.last.serverOpen();
    FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { n: 1 } });
    FakeWebSocket.last.serverSend({ ch: 2, t: 'event', d: { n: 2 } });
    FakeWebSocket.last.serverSend({ ch: 99, t: 'event' });
    FakeWebSocket.last.onmessage?.({ data: 'not json' });
    expect(a.frames).toEqual([{ type: 'snapshot', data: { n: 1 } }]);
    expect(b.frames).toEqual([{ type: 'event', data: { n: 2 } }]);
  });

  it('answers ping with pong and sends channel ops', () => {
    const { socket } = setup();
    const { channel } = collect(socket, 'echo.open', null);
    FakeWebSocket.last.serverOpen();
    FakeWebSocket.last.serverSend({ ch: 0, t: 'ping' });
    FakeWebSocket.last.serverSend({ ch: 0, t: 'other' });
    channel.send('echo.say', { x: 1 });
    channel.send('echo.poke');
    const frames = FakeWebSocket.last.frames;
    expect(frames).toContainEqual({ op: 'pong', ch: 0 });
    expect(frames).toContainEqual({ op: 'echo.say', ch: 1, d: { x: 1 } });
    expect(frames).toContainEqual({ op: 'echo.poke', ch: 1 });
  });

  it('closing a channel sends the close op once and stops delivery', () => {
    const { socket } = setup();
    const { channel, frames } = collect(socket);
    FakeWebSocket.last.serverOpen();
    channel.close();
    channel.close();
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.close')).toEqual([
      { op: 'watch.close', ch: 1 },
    ]);
    FakeWebSocket.last.serverSend({ ch: 1, t: 'event' });
    expect(frames).toEqual([]);
  });

  it('a closed frame from the server ends the channel', () => {
    const { socket } = setup();
    const { channel, frames } = collect(socket);
    FakeWebSocket.last.serverOpen();
    FakeWebSocket.last.serverSend({ ch: 1, t: 'closed', d: { reason: 'slow_consumer' } });
    expect(frames).toEqual([{ type: 'closed', data: { reason: 'slow_consumer' } }]);
    channel.close();
    expect(FakeWebSocket.last.frames.some((f) => f.op === 'watch.close')).toBe(false);
  });

  it('reconnects with back-off 1 s, 2 s, 4 s … 30 s and re-opens every channel', () => {
    const { socket } = setup();
    collect(socket);
    collect(socket, 'logs.open', { pod: 'a' });
    FakeWebSocket.last.serverOpen();

    const delays = [1000, 2000, 4000, 8000, 16000, 30000, 30000];
    delays.forEach((ms, i) => {
      FakeWebSocket.last.serverClose(1006);
      expect(FakeWebSocket.instances).toHaveLength(i + 1);
      vi.advanceTimersByTime(ms - 1);
      expect(FakeWebSocket.instances).toHaveLength(i + 1);
      vi.advanceTimersByTime(1);
      expect(FakeWebSocket.instances).toHaveLength(i + 2);
    });

    FakeWebSocket.last.serverOpen();
    const ops = FakeWebSocket.last.frames.map((f) => `${f.op}:${f.ch}`);
    expect(ops).toEqual(['visibility:0', 'watch.open:1', 'logs.open:2']);

    // a successful connection resets the delay
    FakeWebSocket.last.serverClose(1006);
    vi.advanceTimersByTime(1000);
    expect(FakeWebSocket.instances).toHaveLength(delays.length + 2);
  });

  it('code 4401 ends the session: no reconnect, listeners are told', () => {
    const { socket } = setup();
    const ended = vi.fn();
    const off = socket.onSessionEnded(ended);
    collect(socket);
    FakeWebSocket.last.serverOpen();
    FakeWebSocket.last.serverClose(4401);
    vi.advanceTimersByTime(60_000);
    expect(ended).toHaveBeenCalledTimes(1);
    expect(FakeWebSocket.instances).toHaveLength(1);
    off();
    collect(socket); // no new connection after the session ended
    expect(FakeWebSocket.instances).toHaveLength(1);
  });

  it('tells the server when the tab is hidden or shown, and reports the state on connect', () => {
    const t = setup();
    collect(t.socket);
    FakeWebSocket.last.serverOpen();
    t.setHidden(true);
    t.setHidden(false);
    const visibility = FakeWebSocket.last.frames.filter((f) => f.op === 'visibility');
    expect(visibility.map((f) => f.d)).toEqual([{ hidden: false }, { hidden: true }, { hidden: false }]);
  });

  it('re-opens paused channels (on a new channel number) when the tab becomes visible again', () => {
    const t = setup();
    const { frames } = collect(t.socket);
    collect(t.socket, 'logs.open', { pod: 'a' });
    FakeWebSocket.last.serverOpen();
    t.setHidden(true);
    FakeWebSocket.last.serverSend({ ch: 1, t: 'paused', d: {} });
    expect(frames).toEqual([{ type: 'paused', data: {} }]);

    t.setHidden(false);
    const sent = FakeWebSocket.last.frames.map((f) => `${f.op}:${f.ch}`);
    expect(sent.slice(-3)).toEqual(['visibility:0', 'watch.close:1', 'watch.open:3']);
    FakeWebSocket.last.serverSend({ ch: 3, t: 'snapshot', d: {} });
    expect(frames[frames.length - 1]).toEqual({ type: 'snapshot', data: {} });

    t.setHidden(true);
    t.setHidden(false); // nothing paused any more
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.open')).toHaveLength(2);
  });

  it('a paused flag does not survive a reconnect', () => {
    const t = setup();
    collect(t.socket);
    FakeWebSocket.last.serverOpen();
    FakeWebSocket.last.serverSend({ ch: 1, t: 'paused' });
    FakeWebSocket.last.serverClose(1006);
    vi.advanceTimersByTime(1000);
    FakeWebSocket.last.serverOpen();
    t.setHidden(false);
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.open')).toHaveLength(1);
  });

  it('dispose closes the socket and cancels a pending reconnect', () => {
    const t = setup();
    collect(t.socket);
    FakeWebSocket.last.serverOpen();
    FakeWebSocket.last.serverClose(1006);
    t.socket.dispose();
    vi.advanceTimersByTime(60_000);
    expect(FakeWebSocket.instances).toHaveLength(1);
    expect(t.timers.clearTimeout).toHaveBeenCalled();

    const u = setup();
    collect(u.socket);
    FakeWebSocket.last.serverOpen();
    u.socket.dispose();
    expect(FakeWebSocket.last.readyState).toBe(3);
  });

  it('evaluates open data given as a function at every (re)open', () => {
    const { socket } = setup();
    let since = 'none';
    socket.open('logs.open', () => ({ since }), { onFrame: () => undefined });
    FakeWebSocket.last.serverOpen();
    since = '12:00';
    FakeWebSocket.last.serverClose(1006);
    vi.advanceTimersByTime(1000);
    FakeWebSocket.last.serverOpen();
    const opens = FakeWebSocket.instances.flatMap((w) => w.frames).filter((f) => f.op === 'logs.open');
    expect(opens.map((f) => f.d)).toEqual([{ since: 'none' }, { since: '12:00' }]);
  });

  it('does not send before the connection is open', () => {
    const { socket } = setup();
    const { channel } = collect(socket);
    channel.send('echo.say');
    expect(FakeWebSocket.last.sent).toEqual([]);
  });
});

describe('socketUrl', () => {
  it('uses wss on https and ws otherwise', () => {
    expect(socketUrl({ protocol: 'https:', host: 'k.example.com' })).toBe('wss://k.example.com/api/v1/ws');
    expect(socketUrl({ protocol: 'http:', host: 'localhost:5173' })).toBe('ws://localhost:5173/api/v1/ws');
  });
});
