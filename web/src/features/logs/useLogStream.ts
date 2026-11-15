import { useCallback, useEffect, useRef, useState } from 'react';
import { useSocket } from '../../ws/context';
import type { Channel } from '../../ws/socket';
import { appendLines, markDropped, prependLines, type LogLine } from './logBuffer';
import type { LogSources } from './MultiSourcePicker';

export interface LogRequest {
  cluster: string;
  namespace: string;
  pod: string;
  container: string;
  tailLines?: number;
  sinceSeconds?: number;
  previous?: boolean;
  /** Several containers: replaces `pod` and `container` in the request to the server. */
  sources?: LogSources;
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
  /** Asks the server for older lines of every source that has more; once per source until they arrive. */
  loadOlder(lines?: number): void;
  /** Why no older lines can be had: the start of the log, or the server's cap. */
  historyEnd?: 'start' | 'cap';
  /** A source that could not be opened or failed, as `pod/container: message`. */
  notice?: string;
}

const sourceKey = (pod: string | undefined, container: string | undefined) =>
  `${pod ?? ''}/${container ?? ''}`;

/** Lines asked for per history request. */
export const HISTORY_STEP = 500;

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
  const [historyEnd, setHistoryEnd] = useState<'start' | 'cap'>();
  const [notice, setNotice] = useState<string>();
  const buffer = useRef<LogLine[]>([]);
  const pausedRef = useRef(false);
  const channelRef = useRef<Channel | undefined>(undefined);
  const pending = useRef(new Set<string>());
  const ended = useRef(new Map<string, 'start' | 'cap'>());
  const extra = useRef(0); // lines loaded by history: they do not count against the live buffer size

  const { cluster, namespace, pod, container, tailLines, sinceSeconds, previous, sources } =
    request ??
    ({
      cluster: '',
      namespace: '',
      pod: '',
      container: '',
    } as LogRequest);
  const enabled = request !== undefined;
  const sourcesKey = JSON.stringify(sources ?? null);

  useEffect(() => {
    if (!enabled) return undefined;
    buffer.current = [];
    pausedRef.current = false;
    setLines([]);
    setPaused(false);
    setStatus('connecting');
    setError(undefined);
    setHistoryEnd(undefined);
    setNotice(undefined);
    pending.current = new Set();
    ended.current = new Map();
    extra.current = 0;

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

    // Nothing older can be had once any source hit the server's cap, or every source reached its start.
    const allEnded = (): 'start' | 'cap' | undefined => {
      const reasons = [...ended.current.values()];
      if (reasons.includes('cap')) return 'cap';
      const known = new Set<string>();
      for (const l of buffer.current) {
        if (!l.marker)
          known.add(l.pod === undefined ? sourceKey(pod, container) : sourceKey(l.pod, l.container));
      }
      return known.size > 0 && [...known].every((k) => ended.current.has(k)) ? 'start' : undefined;
    };

    const channel = socket.open(
      'logs.open',
      () => ({
        cluster,
        namespace,
        ...(sources ? { sources } : { pod, container }),
        follow: !previous,
        previous: previous || undefined,
        tailLines: last ? undefined : tailLines,
        sinceSeconds: last ? undefined : sinceSeconds,
        sinceTime: last?.ts,
      }),
      {
        onFrame(type, data) {
          const d = (data ?? {}) as {
            lines?: LogLine[];
            count?: number;
            message?: string;
            pod?: string;
            container?: string;
            reason?: 'start' | 'cap';
          };
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
              buffer.current = appendLines(buffer.current, fresh, maxLines + extra.current);
              publish();
              break;
            }
            case 'history': {
              const older = d.lines ?? [];
              pending.current.delete(sourceKey(d.pod, d.container));
              if (older.length > 0) {
                extra.current += older.length;
                buffer.current = prependLines(buffer.current, older);
                publish();
              }
              break;
            }
            case 'historyEnd': {
              const key = sourceKey(d.pod, d.container);
              pending.current.delete(key);
              ended.current.set(key, d.reason === 'cap' ? 'cap' : 'start');
              setHistoryEnd(allEnded());
              break;
            }
            case 'source_error':
              setNotice(`${d.pod ?? ''}/${d.container ?? ''}: ${d.message ?? 'The log stream failed.'}`);
              break;
            case 'dropped':
              buffer.current = markDropped(buffer.current, d.count ?? 0, maxLines);
              publish();
              break;
            case 'eof':
              if (d.pod === undefined) setStatus('ended'); // one container of several ending does not end the view
              break;
            case 'error':
              setError(d.message ?? 'The log stream failed.');
              setStatus('error');
              break;
          }
        },
      },
    );
    channelRef.current = channel;
    return () => {
      channelRef.current = undefined;
      channel.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `sources` is compared by its JSON in sourcesKey
  }, [
    socket,
    enabled,
    cluster,
    namespace,
    pod,
    container,
    tailLines,
    sinceSeconds,
    previous,
    maxLines,
    sourcesKey,
  ]);

  const pause = useCallback(() => {
    pausedRef.current = true;
    setPaused(true);
  }, []);
  const resume = useCallback(() => {
    pausedRef.current = false;
    setPaused(false);
    setLines(buffer.current);
  }, []);

  const loadOlder = useCallback(
    (count = HISTORY_STEP) => {
      const channel = channelRef.current;
      if (!channel) return;
      // the oldest line of each source in the buffer is where its older lines end
      const oldest = new Map<string, { pod: string; container: string; ts: number; text: string }>();
      for (const l of buffer.current) {
        if (l.marker || l.ts === '') continue;
        const p = l.pod ?? pod;
        const c = l.pod === undefined ? container : (l.container ?? '');
        const key = sourceKey(p, c);
        const ts = Date.parse(l.ts);
        const cur = oldest.get(key);
        if (!cur || ts < cur.ts) oldest.set(key, { pod: p, container: c, ts, text: l.ts });
      }
      for (const [key, o] of oldest) {
        if (pending.current.has(key) || ended.current.has(key)) continue;
        pending.current.add(key);
        channel.send('logs.history', {
          source: { pod: o.pod, container: o.container },
          before: o.text,
          lines: count,
        });
      }
    },
    [pod, container],
  );

  return { lines, status, error, paused, pause, resume, loadOlder, historyEnd, notice };
}
