import { useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '../../components/Button';
import { highlight, matches } from '../../lib/highlight';
import { visibleRange } from '../../lib/window';
import { useLogStream, type LogRequest } from './useLogStream';

const LINE_HEIGHT = 18;
const OVERSCAN = 10;
const TAILS = [100, 500, 1000, 5000];
const SINCE: Array<[string, number]> = [
  ['All time', 0],
  ['Last 5 minutes', 300],
  ['Last hour', 3600],
  ['Last 24 hours', 86400],
];

export type SaveFile = (name: string, blob: Blob) => void;

/** Downloads a blob through a temporary link. */
export function browserSaveFile(name: string, blob: Blob): void {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}

export interface LogViewerProps {
  cluster: string;
  namespace: string;
  pod: string;
  containers: string[];
  /** How many lines the live buffer keeps (`me.limits.logsLiveBufferLines`). */
  maxLines: number;
  saveFile?: SaveFile;
  /** The height of the line list; measured when absent (jsdom has no layout, so tests pass it). */
  viewportHeight?: number;
}

/** One container's log: follows the stream, searches it on the client and downloads what is buffered. */
export function LogViewer({
  cluster,
  namespace,
  pod,
  containers,
  maxLines,
  saveFile = browserSaveFile,
  viewportHeight = 360,
}: LogViewerProps) {
  const [container, setContainer] = useState(containers[0] ?? '');
  const [follow, setFollow] = useState(true);
  const [timestamps, setTimestamps] = useState(true);
  const [tail, setTail] = useState(500);
  const [since, setSince] = useState(0);
  const [previous, setPrevious] = useState(false);
  const [query, setQuery] = useState('');
  const [regex, setRegex] = useState(false);
  const [scrollTop, setScrollTop] = useState(0);
  const [atBottom, setAtBottom] = useState(true);
  const list = useRef<HTMLDivElement>(null);

  const request = useMemo<LogRequest>(
    () => ({
      cluster,
      namespace,
      pod,
      container,
      tailLines: tail,
      sinceSeconds: since || undefined,
      previous,
    }),
    [cluster, namespace, pod, container, tail, since, previous],
  );
  const stream = useLogStream(request, maxLines);

  const regexError = regex && query !== '' ? highlight('', query, true).error : undefined;
  const visible = useMemo(
    () =>
      query === '' ? stream.lines : stream.lines.filter((l) => l.marker || matches(l.text, query, regex)),
    [stream.lines, query, regex],
  );

  const [start, end] = visibleRange({
    rowCount: visible.length,
    rowHeight: LINE_HEIGHT,
    viewportHeight,
    scrollTop,
    overscan: OVERSCAN,
  });

  // Follow only while the reader is at the bottom: scrolling up lets go of the tail.
  useEffect(() => {
    const el = list.current;
    if (follow && atBottom && el) {
      el.scrollTop = visible.length * LINE_HEIGHT;
      setScrollTop(el.scrollTop);
    }
  }, [visible.length, follow, atBottom]);

  const download = () => {
    const text = stream.lines
      .filter((l) => !l.marker)
      .map((l) => (timestamps ? `${l.ts} ${l.text}` : l.text))
      .join('\n');
    saveFile(`${pod}-${container}.log`, new Blob([text + '\n'], { type: 'text/plain' }));
  };

  return (
    <div className="log-viewer">
      <div className="log-controls">
        <label>
          Container
          <select value={container} onChange={(e) => setContainer(e.target.value)}>
            {containers.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
        </label>
        <label>
          <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} /> Follow
        </label>
        <label>
          <input type="checkbox" checked={timestamps} onChange={(e) => setTimestamps(e.target.checked)} />{' '}
          Timestamps
        </label>
        <label>
          Tail
          <select value={tail} onChange={(e) => setTail(Number(e.target.value))}>
            {TAILS.map((n) => (
              <option key={n} value={n}>
                {n} lines
              </option>
            ))}
          </select>
        </label>
        <label>
          Since
          <select value={since} onChange={(e) => setSince(Number(e.target.value))}>
            {SINCE.map(([label, seconds]) => (
              <option key={seconds} value={seconds}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label>
          <input type="checkbox" checked={previous} onChange={(e) => setPrevious(e.target.checked)} />{' '}
          Previous instance
        </label>
        <label>
          Search
          <input type="search" value={query} onChange={(e) => setQuery(e.target.value)} />
        </label>
        <label>
          <input type="checkbox" checked={regex} onChange={(e) => setRegex(e.target.checked)} /> Regex
        </label>
        {regexError && <span role="alert">Invalid regular expression</span>}
        <Button onClick={stream.paused ? stream.resume : stream.pause}>
          {stream.paused ? 'Resume' : 'Pause'}
        </Button>
        <Button onClick={download}>Download</Button>
      </div>
      {stream.status === 'connecting' && <p role="status">Connecting…</p>}
      {stream.status === 'ended' && <p role="status">The log ended.</p>}
      {stream.status === 'error' && <p role="alert">{stream.error}</p>}
      <div
        ref={list}
        className="log-lines"
        role="log"
        aria-label={`Logs of ${pod}`}
        style={{ height: viewportHeight, overflowY: 'auto' }}
        onScroll={(e) => {
          const el = e.currentTarget;
          setScrollTop(el.scrollTop);
          setAtBottom(el.scrollHeight - el.scrollTop - viewportHeight <= LINE_HEIGHT);
        }}
      >
        <div style={{ height: visible.length * LINE_HEIGHT, position: 'relative' }}>
          {visible.slice(start, end).map((l, offset) => {
            const index = start + offset;
            const { segments } = highlight(l.text, query, regex);
            return (
              <div
                key={index}
                className={l.marker ? 'log-line log-marker' : 'log-line'}
                style={{ position: 'absolute', top: index * LINE_HEIGHT, height: LINE_HEIGHT }}
              >
                {timestamps && !l.marker && <span className="log-ts">{l.ts} </span>}
                {segments.map((s, i) =>
                  s.match ? <mark key={i}>{s.text}</mark> : <span key={i}>{s.text}</span>,
                )}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
