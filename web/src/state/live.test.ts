import { beforeEach, describe, expect, it } from 'vitest';
import { FakeWebSocket } from '../test/FakeWebSocket';
import { createSocket, type WebSocketCtor } from '../ws/socket';
import { createLiveStore, keyId, type LiveKey } from './live';

const key: LiveKey = { cluster: 'v:prod', group: 'core', version: 'v1', resource: 'pods', namespace: '' };
const pod = (uid: string, name: string, extra: Record<string, unknown> = {}) => ({
  metadata: { uid, name, namespace: 'ns' },
  ...extra,
});

function setup() {
  FakeWebSocket.reset();
  const socket = createSocket({
    url: 'ws://x/api/v1/ws',
    WebSocketCtor: FakeWebSocket as unknown as WebSocketCtor,
    timers: { setTimeout: () => 0, clearTimeout: () => undefined },
    visibility: { isHidden: () => false, subscribe: () => () => undefined },
  });
  const store = createLiveStore(socket);
  const entry = (k: LiveKey = key) => store.getState().entries[keyId(k)];
  return { store, entry };
}

describe('live store', () => {
  beforeEach(() => FakeWebSocket.reset());

  it('opens a watch channel and starts as loading', () => {
    const { store, entry } = setup();
    store.getState().subscribe(key);
    FakeWebSocket.last.serverOpen();
    expect(FakeWebSocket.last.frames[1]).toEqual({
      op: 'watch.open',
      ch: 1,
      d: { cluster: 'v:prod', group: 'core', version: 'v1', resource: 'pods', namespace: '' },
    });
    expect(entry()).toEqual({ status: 'loading', items: [] });
  });

  it('a field selector is part of the key and is sent with watch.open', () => {
    const { store, entry } = setup();
    const one: LiveKey = { ...key, namespace: 'ns', fieldSelector: 'metadata.name=a' };
    expect(keyId(one)).toBe(`${keyId({ ...key, namespace: 'ns' })}|metadata.name=a`);
    store.getState().subscribe(one);
    FakeWebSocket.last.serverOpen();
    expect(FakeWebSocket.last.frames[1]?.d).toEqual({
      cluster: 'v:prod',
      group: 'core',
      version: 'v1',
      resource: 'pods',
      namespace: 'ns',
      fieldSelector: 'metadata.name=a',
    });
    expect(entry(one)).toEqual({ status: 'loading', items: [] });
  });

  it('a snapshot replaces the list and the status becomes live', () => {
    const { store, entry } = setup();
    store.getState().subscribe(key);
    FakeWebSocket.last.serverOpen();
    FakeWebSocket.last.serverSend({ ch: 1, t: 'opened' });
    FakeWebSocket.last.serverSend({
      ch: 1,
      t: 'snapshot',
      d: { items: [pod('1', 'a'), pod('2', 'b')], resourceVersion: '5', more: false },
    });
    expect(entry()?.status).toBe('live');
    expect(entry()?.items).toHaveLength(2);
    FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { items: [pod('3', 'c')], more: false } });
    expect(entry()?.items).toEqual([pod('3', 'c')]);
  });

  it('a multi-frame snapshot is applied when the last frame arrives', () => {
    const { store, entry } = setup();
    store.getState().subscribe(key);
    FakeWebSocket.last.serverOpen();
    FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { items: [pod('1', 'a')], more: true } });
    expect(entry()?.status).toBe('loading');
    expect(entry()?.items).toEqual([]);
    FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { items: [pod('2', 'b')], more: true } });
    FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { items: [pod('3', 'c')], more: false } });
    expect(entry()?.status).toBe('live');
    expect(entry()?.items.map((o) => (o.metadata as { name: string }).name)).toEqual(['a', 'b', 'c']);
    // a later snapshot does not append to the first one
    FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { items: [pod('9', 'z')], more: false } });
    expect(entry()?.items).toHaveLength(1);
  });

  it('events add, replace by uid and remove', () => {
    const { store, entry } = setup();
    store.getState().subscribe(key);
    FakeWebSocket.last.serverOpen();
    const send = (type: string, object: unknown) =>
      FakeWebSocket.last.serverSend({ ch: 1, t: 'event', d: { type, object } });
    FakeWebSocket.last.serverSend({
      ch: 1,
      t: 'snapshot',
      d: { items: [pod('1', 'a'), pod('2', 'b')], more: false },
    });
    send('ADDED', pod('3', 'c'));
    send('MODIFIED', pod('1', 'a', { status: { phase: 'Running' } }));
    expect(entry()?.items.map((o) => (o.metadata as { name: string }).name)).toEqual(['a', 'b', 'c']);
    expect(entry()?.items[0]).toHaveProperty('status.phase', 'Running');
    send('DELETED', pod('2', 'b'));
    send('DELETED', pod('42', 'unknown'));
    expect(entry()?.items.map((o) => (o.metadata as { name: string }).name)).toEqual(['a', 'c']);
    send('MODIFIED', pod('3', 'c', { x: 1 })); // the index survived the removal
    expect(entry()?.items[1]).toHaveProperty('x', 1);
    FakeWebSocket.last.serverSend({ ch: 1, t: 'event', d: { type: 'ADDED' } }); // no object: ignored
    expect(entry()?.items).toHaveLength(2);
  });

  it('objects without a uid are keyed by namespace and name', () => {
    const { store, entry } = setup();
    store.getState().subscribe(key);
    FakeWebSocket.last.serverOpen();
    const bare = (name: string, x: number) => ({ metadata: { namespace: 'ns', name }, x });
    FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { items: [bare('a', 1)], more: false } });
    FakeWebSocket.last.serverSend({ ch: 1, t: 'event', d: { type: 'MODIFIED', object: bare('a', 2) } });
    expect(entry()?.items).toEqual([bare('a', 2)]);
  });

  it('paused and error frames set the status and keep or explain the list', () => {
    const { store, entry } = setup();
    store.getState().subscribe(key);
    FakeWebSocket.last.serverOpen();
    FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { items: [pod('1', 'a')], more: false } });
    FakeWebSocket.last.serverSend({ ch: 1, t: 'paused', d: {} });
    expect(entry()?.status).toBe('paused');
    expect(entry()?.items).toHaveLength(1);
    FakeWebSocket.last.serverSend({ ch: 1, t: 'error', d: { code: 'forbidden', message: 'no' } });
    expect(entry()?.status).toBe('error');
    expect(entry()?.error).toEqual({ code: 'forbidden', message: 'no' });
    FakeWebSocket.last.serverSend({ ch: 1, t: 'error' });
    expect(entry()?.error).toEqual({ code: 'internal', message: 'Something went wrong.' });
  });

  it('two subscribers of the same key share one channel, closed when the last one leaves', () => {
    const { store, entry } = setup();
    const a = store.getState().subscribe(key);
    const b = store.getState().subscribe(key);
    FakeWebSocket.last.serverOpen();
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.open')).toHaveLength(1);
    a();
    a(); // leaving twice counts once
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.close')).toHaveLength(0);
    b();
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.close')).toEqual([
      { op: 'watch.close', ch: 1 },
    ]);
    expect(entry()).toBeUndefined();

    store.getState().subscribe(key); // a later subscription opens a new channel
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.open')).toHaveLength(2);
  });

  it('different keys use different channels', () => {
    const { store } = setup();
    store.getState().subscribe(key);
    store.getState().subscribe({ ...key, namespace: 'ns1' });
    FakeWebSocket.last.serverOpen();
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.open')).toHaveLength(2);
  });

  it('a channel the server dropped for being too slow is opened again', () => {
    const { store, entry } = setup();
    store.getState().subscribe(key);
    FakeWebSocket.last.serverOpen();
    FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { items: [pod('1', 'a')], more: false } });
    FakeWebSocket.last.serverSend({ ch: 1, t: 'closed', d: { reason: 'slow_consumer' } });
    expect(entry()).toEqual({ status: 'loading', items: [] });
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.open').map((f) => f.ch)).toEqual([1, 2]);
    FakeWebSocket.last.serverSend({ ch: 2, t: 'snapshot', d: { items: [pod('1', 'a')], more: false } });
    expect(entry()?.status).toBe('live');
    FakeWebSocket.last.serverSend({ ch: 2, t: 'closed', d: { reason: 'error' } }); // other reasons end the channel
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'watch.open')).toHaveLength(2);
  });

  it('keyId treats an empty group as core', () => {
    expect(keyId({ ...key, group: '' })).toBe(keyId(key));
  });
});
