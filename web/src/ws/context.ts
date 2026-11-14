import { createContext, useContext } from 'react';
import { browserSocket, type Socket } from './socket';

let appSocketInstance: Socket | undefined;

/** The one socket of this tab, created on first use. */
export function appSocket(): Socket {
  appSocketInstance ??= browserSocket();
  return appSocketInstance;
}

export const SocketContext = createContext<Socket | null>(null);

export function useSocket(): Socket {
  return useContext(SocketContext) ?? appSocket();
}
