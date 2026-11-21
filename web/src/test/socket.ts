import { createSocket, type Socket, type WebSocketCtor } from '../ws/socket';
import { FakeWebSocket } from './FakeWebSocket';

/** A socket on a fake WebSocket; the test plays the server through `FakeWebSocket.last`. */
export function makeSocket(): Socket {
  FakeWebSocket.reset();
  return createSocket({
    url: 'ws://test/api/v1/ws',
    WebSocketCtor: FakeWebSocket as unknown as WebSocketCtor,
    timers: { setTimeout: () => 0, clearTimeout: () => undefined },
    visibility: { isHidden: () => false, subscribe: () => () => undefined },
  });
}
