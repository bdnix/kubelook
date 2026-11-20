import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it } from 'vitest';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { SocketContext } from '../../ws/context';
import { createSocket, type WebSocketCtor } from '../../ws/socket';
import { useResourceMap, type MapTarget } from './useResourceMap';

function setup(target: MapTarget | undefined) {
  FakeWebSocket.reset();
  const socket = createSocket({
    url: 'ws://t/api/v1/ws',
    WebSocketCtor: FakeWebSocket as unknown as WebSocketCtor,
    timers: { setTimeout: () => 0, clearTimeout: () => undefined },
    visibility: { isHidden: () => false, subscribe: () => () => undefined },
  });
  const wrapper = ({ children }: { children: ReactNode }) => <SocketContext.Provider value={socket}>{children}</SocketContext.Provider>;
  return renderHook(({ t }) => useResourceMap(t), { wrapper, initialProps: { t: target } });
}

const graph = { nodes: [{ id: 'Pod/ns/a', group: '', kind: 'Pod', namespace: 'ns', name: 'a', status: 'Running' }], edges: [], omittedKinds: ['Secret'], leftOutNamespaces: ['z'] };

beforeEach(() => FakeWebSocket.reset());

describe('useResourceMap', () => {
  it('opens a map channel with the cluster, the namespaces and the focus', () => {
    const focus = { group: 'apps', kind: 'Deployment', namespace: 'ns', name: 'web' };
    const { result } = setup({ cluster: 'v:prod', namespaces: ['ns'], focus });
    act(() => FakeWebSocket.last.serverOpen());
    expect(FakeWebSocket.last.frames.find((f) => f.op === 'map.open')).toEqual({ op: 'map.open', ch: 1, d: { cluster: 'v:prod', namespaces: ['ns'], focus } });
    expect(result.current.status).toBe('loading');
  });

  it('keeps the last graph frame', () => {
    const { result } = setup({ cluster: 'c', namespaces: [] });
    act(() => FakeWebSocket.last.serverOpen());
    act(() => FakeWebSocket.last.serverSend({ ch: 1, t: 'opened' }));
    act(() => FakeWebSocket.last.serverSend({ ch: 1, t: 'graph', d: graph }));
    expect(result.current.status).toBe('ready');
    expect(result.current.graph?.nodes).toHaveLength(1);
    expect(result.current.graph?.omittedKinds).toEqual(['Secret']);
    act(() => FakeWebSocket.last.serverSend({ ch: 1, t: 'graph', d: { ...graph, nodes: [] } }));
    expect(result.current.graph?.nodes).toHaveLength(0);
  });

  it('reports an error frame', () => {
    const { result } = setup({ cluster: 'c', namespaces: [] });
    act(() => FakeWebSocket.last.serverOpen());
    act(() => FakeWebSocket.last.serverSend({ ch: 1, t: 'error', d: { code: 'forbidden', message: 'No access.' } }));
    expect(result.current.status).toBe('error');
    expect(result.current.error).toBe('No access.');
  });

  it('opens again when the namespaces change, and closes the old channel', () => {
    const { rerender } = setup({ cluster: 'c', namespaces: ['a'] });
    act(() => FakeWebSocket.last.serverOpen());
    rerender({ t: { cluster: 'c', namespaces: ['a', 'b'] } });
    const ops = FakeWebSocket.last.frames.map((f) => f.op);
    expect(ops.filter((o) => o === 'map.open')).toHaveLength(2);
    expect(ops).toContain('map.close');
  });

  it('does nothing without a target', () => {
    const { result } = setup(undefined);
    expect(FakeWebSocket.instances).toHaveLength(0);
    expect(result.current.status).toBe('idle');
  });
});
