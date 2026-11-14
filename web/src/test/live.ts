import { createLiveStore } from '../state/live';
import { createSocket, type WebSocketCtor } from '../ws/socket';
import { FakeWebSocket } from './FakeWebSocket';

/** A live store on a fake WebSocket: the test plays the server through `FakeWebSocket.last`. */
export function makeLiveStore() {
  FakeWebSocket.reset();
  const socket = createSocket({
    url: 'ws://test/api/v1/ws',
    WebSocketCtor: FakeWebSocket as unknown as WebSocketCtor,
    timers: { setTimeout: () => 0, clearTimeout: () => undefined },
    visibility: { isHidden: () => false, subscribe: () => () => undefined },
  });
  return createLiveStore(socket);
}
