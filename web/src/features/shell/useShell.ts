import { useCallback, useEffect, useState } from 'react';
import type { TerminalHandle } from '../../adapters/xterm/Terminal';
import { base64ToBytes, bytesToBase64, encodeUtf8 } from '../../lib/bytes';
import { useSocket } from '../../ws/context';

export interface ShellRequest {
  cluster: string;
  namespace: string;
  pod: string;
  container: string;
}

export type ShellStatus = 'connecting' | 'running' | 'exited' | 'lost' | 'error';

export const LOST_MESSAGE = 'The server connection was lost and the shell session ended.';

export interface Shell {
  status: ShellStatus;
  /** The shell that started, such as `/bin/bash`. */
  shell?: string;
  exitCode?: number;
  error?: string;
  /** Gives the hook the terminal; the shell opens once it is there. */
  attach(handle: TerminalHandle): void;
  /** Starts a new shell after the old one ended. */
  restart(): void;
}

/** One browser shell: terminal input goes to the process, its output to the terminal, resizes follow. */
export function useShell(request: ShellRequest): Shell {
  const socket = useSocket();
  const [handle, setHandle] = useState<TerminalHandle>();
  const [generation, setGeneration] = useState(0);
  const [status, setStatus] = useState<ShellStatus>('connecting');
  const [shell, setShell] = useState<string>();
  const [exitCode, setExitCode] = useState<number>();
  const [error, setError] = useState<string>();
  const { cluster, namespace, pod, container } = request;

  useEffect(() => {
    if (!handle) return undefined;
    setStatus('connecting');
    setShell(undefined);
    setExitCode(undefined);
    setError(undefined);
    let open = true;
    const { cols, rows } = handle.size();
    const channel = socket.open(
      'exec.open',
      { cluster, namespace, pod, container, cols, rows },
      {
        onFrame(type, data) {
          const d = (data ?? {}) as { shell?: string; data?: string; code?: number; message?: string };
          switch (type) {
            case 'opened':
              setShell(d.shell);
              setStatus('running');
              break;
            case 'out':
              handle.write(base64ToBytes(d.data ?? ''));
              break;
            case 'exit':
              setExitCode(d.code ?? 0);
              setStatus('exited');
              break;
            case 'error':
              setError(d.message ?? 'The shell failed.');
              setStatus('error');
              break;
            case 'closed':
              open = false;
              setStatus((s) => (s === 'running' || s === 'connecting' ? 'exited' : s));
              break;
          }
        },
        onConnectionLost() {
          open = false;
          setStatus('lost');
          setError(LOST_MESSAGE);
        },
      },
    );
    handle.onData((text) => {
      if (open) channel.send('exec.stdin', { data: bytesToBase64(encodeUtf8(text)) });
    });
    handle.onResize((c, r) => {
      if (open) channel.send('exec.resize', { cols: c, rows: r });
    });
    return () => {
      open = false;
      channel.close();
    };
  }, [socket, handle, generation, cluster, namespace, pod, container]);

  const attach = useCallback((h: TerminalHandle) => setHandle(h), []);
  const restart = useCallback(() => setGeneration((g) => g + 1), []);
  return { status, shell, exitCode, error, attach, restart };
}
