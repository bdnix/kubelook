import { useCallback, useEffect, useRef, useState } from 'react';
import { useSocket } from '../../ws/context';
import { appendLines, markDropped, type LogLine } from './logBuffer';

export interface LogRequest {
  cluster: string;
  namespace: string;
  pod: string;
  container: string;
  tailLines?: number;
  sinceSeconds?: number;
  previous?: boolean;
}

export type LogStatus = 'connecting' | 'streaming' | 'ended' | 'error';

export interface LogStream {
  /** What the view shows: frozen while paused. */
  lines: LogLine[];
  status: LogStatus;
  error?: string;
  paused: boolean;
  pause(): void;
  resume(): void;
}

interface Last {
  ts: string;
  text: string;
}

/** Follows one container's log. A reopened channel resumes after the last line it saw. */
export function useLogStream(request: LogRequest | undefined, maxLines: number): LogStream {
  const socket = useSocket();
  const [lines, setLines] = useState<LogLine[]>([]);
  const [status, setStatus] = useState<LogStatus>('connecting');
  const [error, setError] = useState<string>();
  const [paused, setPaused] = useState(false);
  const buffer = useRef<LogLine[]>([]);
  const pausedRef = useRef(false);

  const { cluster, namespace, pod, container, tailLines, sinceSeconds, previous } =
    request ??
    ({
      cluster: '',
      namespace: '',
      pod: '',
      container: '',
    } as LogRequest);
  const enabled = request !== undefined;

  useEffect(() => {
    if (!enabled) return undefined;
    buffer.current = [];
    pausedRef.current = false;
    setLines([]);
    setPaused(false);
    setStatus('connecting');
    setError(undefined);

    let last: Last | undefined;
    let skipping: Last | undefined; // lines already seen before a reconnect

    const publish = () => {
      if (!pausedRef.current) setLines(buffer.current);
    };
    const accept = (incoming: LogLine[]): LogLine[] => {
      if (!skipping) return incoming;
      const out: LogLine[] = [];
      for (const l of incoming) {
        if (skipping) {
          const older = Date.parse(l.ts) < Date.parse(skipping.ts);
          const same = l.ts === skipping.ts && l.text === skipping.text;
          if (older) continue;
          skipping = undefined;
          if (same) continue;
        }
        out.push(l);
      }
      return out;
    };

    const channel = socket.open(
      'logs.open',
      () => ({
        cluster,
        namespace,
        pod,
        container,
        follow: !previous,
        previous: previous || undefined,
        tailLines: last ? undefined : tailLines,
        sinceSeconds: last ? undefined : sinceSeconds,
        sinceTime: last?.ts,
      }),
      {
        onFrame(type, data) {
          const d = (data ?? {}) as { lines?: LogLine[]; count?: number; message?: string };
          switch (type) {
            case 'opened':
              setStatus('streaming');
              skipping = last;
              break;
            case 'lines': {
              const fresh = accept(d.lines ?? []);
              if (fresh.length === 0) return;
              const lastLine = fresh[fresh.length - 1];
              if (lastLine) last = { ts: lastLine.ts, text: lastLine.text };
              buffer.current = appendLines(buffer.current, fresh, maxLines);
              publish();
              break;
            }
            case 'dropped':
              buffer.current = markDropped(buffer.current, d.count ?? 0, maxLines);
              publish();
              break;
            case 'eof':
              setStatus('ended');
              break;
            case 'error':
              setError(d.message ?? 'The log stream failed.');
              setStatus('error');
              break;
          }
        },
      },
    );
    return () => channel.close();
  }, [socket, enabled, cluster, namespace, pod, container, tailLines, sinceSeconds, previous, maxLines]);

  const pause = useCallback(() => {
    pausedRef.current = true;
    setPaused(true);
  }, []);
  const resume = useCallback(() => {
    pausedRef.current = false;
    setPaused(false);
    setLines(buffer.current);
  }, []);

  return { lines, status, error, paused, pause, resume };
}
