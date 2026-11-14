import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it } from 'vitest';
import { base64ToBytes, bytesToBase64, encodeUtf8 } from '../../lib/bytes';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { SocketContext } from '../../ws/context';
import { createSocket, type WebSocketCtor } from '../../ws/socket';
import { CHUNK_BYTES, FileError, useFiles, type FileTarget } from './useFiles';

const target: FileTarget = { cluster: 'v:prod', namespace: 'ns', pod: 'web-1', container: 'app' };

function setup(t: FileTarget | undefined = target) {
  FakeWebSocket.reset();
  const socket = createSocket({
    url: 'ws://t/api/v1/ws',
    WebSocketCtor: FakeWebSocket as unknown as WebSocketCtor,
    timers: { setTimeout: () => 0, clearTimeout: () => undefined },
    visibility: { isHidden: () => false, subscribe: () => () => undefined },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <SocketContext.Provider value={socket}>{children}</SocketContext.Provider>
  );
  const view = renderHook(({ t }) => useFiles(t), { wrapper, initialProps: { t } });
  if (t) {
    act(() => FakeWebSocket.last.serverOpen());
    act(() => FakeWebSocket.last.serverSend({ ch: 1, t: 'opened' }));
  }
  return view;
}

const sent = (op: string) => FakeWebSocket.last.frames.filter((f) => f.op === op);
const reply = (t: string, d: Record<string, unknown>) =>
  act(() => FakeWebSocket.last.serverSend({ ch: 1, t, d }));
const ridOf = (op: string, n = 0) => (sent(op)[n]?.d as { rid: string }).rid;

beforeEach(() => FakeWebSocket.reset());

describe('useFiles', () => {
  it('opens a channel for the container and becomes ready', () => {
    const { result } = setup();
    expect(FakeWebSocket.last.frames[1]).toEqual({ op: 'file.open', ch: 1, d: target });
    expect(result.current.status).toBe('ready');
  });

  it('does nothing without a target', () => {
    FakeWebSocket.reset();
    const socket = createSocket({
      url: 'ws://t/api/v1/ws',
      WebSocketCtor: FakeWebSocket as unknown as WebSocketCtor,
      timers: { setTimeout: () => 0, clearTimeout: () => undefined },
      visibility: { isHidden: () => false, subscribe: () => () => undefined },
    });
    renderHook(() => useFiles(undefined), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <SocketContext.Provider value={socket}>{children}</SocketContext.Provider>
      ),
    });
    expect(FakeWebSocket.instances).toHaveLength(0);
  });

  it('list answers with the entries of the reply to its own request id', async () => {
    const { result } = setup();
    const p = result.current.list('/etc');
    const rid = ridOf('file.list');
    expect(sent('file.list')[0]?.d).toEqual({ rid, path: '/etc' });
    reply('list', { rid: 'other', entries: [], skipped: 9 });
    reply('list', {
      rid,
      entries: [{ name: 'a', type: 'file', mode: '-rw-r--r--', size: 1, modified: 'x' }],
      skipped: 2,
    });
    expect(await p).toEqual({
      entries: [{ name: 'a', type: 'file', mode: '-rw-r--r--', size: 1, modified: 'x' }],
      skipped: 2,
    });
  });

  it('read decodes the content', async () => {
    const { result } = setup();
    const p = result.current.read('/etc/hosts');
    reply('content', {
      rid: ridOf('file.read'),
      data: bytesToBase64(encodeUtf8('hi')),
      binary: false,
      truncated: true,
      stat: { size: 2, modified: 'm' },
    });
    const c = await p;
    expect(Array.from(c.data)).toEqual(Array.from(encodeUtf8('hi')));
    expect(c).toMatchObject({ binary: false, truncated: true, stat: { size: 2, modified: 'm' } });
  });

  it('save sends the bytes as base64 with the expected stat', async () => {
    const { result } = setup();
    const p = result.current.save('/etc/hosts', encodeUtf8('new'), { size: 2, modified: 'm' });
    const frame = sent('file.save')[0]?.d as { path: string; data: string; expect: unknown };
    expect(frame).toMatchObject({
      path: '/etc/hosts',
      data: bytesToBase64(encodeUtf8('new')),
      expect: { size: 2, modified: 'm' },
    });
    reply('saved', { rid: ridOf('file.save'), stat: { size: 3, modified: 'n' } });
    expect(await p).toEqual({ size: 3, modified: 'n' });
    result.current.save('/new', encodeUtf8('x'), undefined).catch(() => undefined);
    expect(sent('file.save')[1]?.d).not.toHaveProperty('expect');
  });

  it('remove sends the recursive flag', async () => {
    const { result } = setup();
    const p = result.current.remove('/tmp/x', true);
    expect(sent('file.delete')[0]?.d).toMatchObject({ path: '/tmp/x', recursive: true });
    reply('deleted', { rid: ridOf('file.delete') });
    await expect(p).resolves.toBeUndefined();
  });

  it('download joins the chunks and reports progress', async () => {
    const { result } = setup();
    const progress: number[] = [];
    const p = result.current.download('/var/log/a', (n) => progress.push(n));
    const rid = ridOf('file.download');
    reply('chunk', { rid, seq: 0, data: bytesToBase64(encodeUtf8('abc')) });
    reply('chunk', { rid, seq: 1, data: bytesToBase64(encodeUtf8('de')) });
    reply('done', { rid, bytes: 5, tar: true });
    const got = await p;
    expect(new TextDecoder().decode(got.data)).toBe('abcde');
    expect(got.tar).toBe(true);
    expect(progress).toEqual([3, 5]);
  });

  it('upload sends begin, numbered 48 KiB chunks and end, and resolves on uploaded', async () => {
    const { result } = setup();
    const data = new Uint8Array(CHUNK_BYTES * 2 + 10).fill(65);
    const sentSizes: number[] = [];
    const p = result.current.upload('/tmp', 'a.bin', data, (n) => sentSizes.push(n));
    expect(sent('file.upload.begin')[0]?.d).toEqual({ dir: '/tmp', name: 'a.bin' });
    const chunks = sent('file.upload.chunk').map((f) => f.d as { seq: number; data: string });
    expect(chunks.map((c) => c.seq)).toEqual([0, 1, 2]);
    expect(chunks.map((c) => base64ToBytes(c.data).length)).toEqual([CHUNK_BYTES, CHUNK_BYTES, 10]);
    expect(sentSizes).toEqual([CHUNK_BYTES, CHUNK_BYTES * 2, CHUNK_BYTES * 2 + 10]);
    reply('uploaded', { rid: ridOf('file.upload.end'), bytes: data.length });
    await expect(p).resolves.toBeUndefined();
  });

  it('a tar upload says tar instead of a name', async () => {
    const { result } = setup();
    const p = result.current.upload('/tmp', undefined, new Uint8Array(3));
    expect(sent('file.upload.begin')[0]?.d).toEqual({ dir: '/tmp', tar: true });
    reply('uploaded', { rid: ridOf('file.upload.end'), bytes: 3 });
    await p;
  });

  it('an error with a request id rejects that request with its code and message', async () => {
    const { result } = setup();
    const p = result.current.read('/x');
    reply('error', { rid: ridOf('file.read'), code: 'not_found', message: 'No such file' });
    await expect(p).rejects.toMatchObject({ name: 'FileError', code: 'not_found', message: 'No such file' });
  });

  it('an error without a request id (a refused chunk) rejects the running upload', async () => {
    const { result } = setup();
    const p = result.current.upload('/tmp', 'a', new Uint8Array(3));
    reply('error', { code: 'too_large', message: 'The upload is larger than the limit' });
    await expect(p).rejects.toMatchObject({ code: 'too_large' });
  });

  it('an error that belongs to nothing fails every request', async () => {
    const { result } = setup();
    const p = result.current.list('/');
    reply('error', { code: 'forbidden', message: 'nope' });
    await expect(p).rejects.toBeInstanceOf(FileError);
  });

  it('a default message when the error has none', async () => {
    const { result } = setup();
    const p = result.current.list('/');
    reply('error', { rid: ridOf('file.list') });
    await expect(p).rejects.toMatchObject({ code: 'internal', message: 'The file operation failed.' });
  });

  it('a lost connection rejects what is pending and reconnect opens a new channel', async () => {
    const { result } = setup();
    const p = result.current.list('/');
    act(() => FakeWebSocket.last.close(1006));
    await expect(p).rejects.toMatchObject({ code: 'lost' });
    expect(result.current.status).toBe('lost');
    act(() => result.current.reconnect());
    expect(result.current.status).toBe('connecting');
  });

  it('the server closing the channel rejects what is pending', async () => {
    const { result } = setup();
    const p = result.current.list('/');
    reply('closed', { reason: 'slow_consumer' });
    await expect(p).rejects.toMatchObject({ code: 'closed' });
  });

  it('calls after unmount fail, and unmounting closes the channel', async () => {
    const { result, unmount } = setup();
    const { list } = result.current;
    const p = list('/');
    unmount();
    await expect(p).rejects.toMatchObject({ code: 'closed' });
    expect(sent('file.close')).toHaveLength(1);
    await expect(list('/')).rejects.toMatchObject({ code: 'closed' });
    await expect(result.current.upload('/', 'a', new Uint8Array())).rejects.toMatchObject({ code: 'closed' });
  });

  it('ignores replies for unknown request ids', () => {
    setup();
    expect(() => reply('list', { rid: 'nobody', entries: [] })).not.toThrow();
  });
});
