import { useEffect, useMemo, useRef, useState } from 'react';
import { useApi } from '../../api/context';
import { Button } from '../../components/Button';
import { highlight, matches } from '../../lib/highlight';
import { visibleRange } from '../../lib/window';
import { exportLogs, type ExportFormat } from './exportLogs';
import { FieldFilter } from './FieldFilter';
import { filterLines, levelClass, levelLabel, parsed, sourceColor, type LineFilter } from './levels';
import type { LogLine } from './logBuffer';
import { LogText } from './LogText';
import { MultiSourcePicker, type LogSources } from './MultiSourcePicker';
import { useLogStream, type LogRequest } from './useLogStream';
import './logs.css';

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

const HISTORY_TRIGGER = LINE_HEIGHT; // scrolled this close to the top asks for older lines

const sourceLabel = (l: LogLine) => (l.pod === undefined ? undefined : `${l.pod}/${l.container ?? ''}`);

/** The fields of a JSON line other than the ones shown separately, as `key=value` text. */
function otherFields(fields: Record<string, unknown>): string {
  const shown = new Set([
    'level',
    'lvl',
    'severity',
    'loglevel',
    'msg',
    'message',
    'time',
    'ts',
    'timestamp',
    '@timestamp',
  ]);
  return Object.entries(fields)
    .filter(([k]) => !shown.has(k))
    .map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`)
    .join(' ');
}

/** The log of one pod's containers: follows the stream, structures JSON lines, searches and filters, loads older lines and exports. */
export function LogViewer({
  cluster,
  namespace,
  pod,
  containers,
  maxLines,
  saveFile = browserSaveFile,
  viewportHeight = 360,
}: LogViewerProps) {
  const api = useApi();
  const [source, setSource] = useState<LogSources>({ pod, container: containers[0] ?? '' });
  const [follow, setFollow] = useState(true);
  const [timestamps, setTimestamps] = useState(true);
  const [tail, setTail] = useState(500);
  const [since, setSince] = useState(0);
  const [previous, setPrevious] = useState(false);
  const [query, setQuery] = useState('');
  const [regex, setRegex] = useState(false);
  const [filter, setFilter] = useState<LineFilter>({});
  const [format, setFormat] = useState<ExportFormat>('text');
  const [exportError, setExportError] = useState<string>();
  const [scrollTop, setScrollTop] = useState(0);
  const [atBottom, setAtBottom] = useState(true);
  const list = useRef<HTMLDivElement>(null);

  const single = source.pod !== undefined && source.container !== undefined && !source.allContainers;
  const container = single ? (source.container ?? '') : '';
  const request = useMemo<LogRequest>(
    () => ({
      cluster,
      namespace,
      pod,
      container,
      sources: single ? undefined : source,
      tailLines: tail,
      sinceSeconds: since || undefined,
      previous,
    }),
    [cluster, namespace, pod, container, single, source, tail, since, previous],
  );
  const stream = useLogStream(request, maxLines);

  const regexError = regex && query !== '' ? highlight('', query, true).error : undefined;
  const visible = useMemo(() => {
    const searched =
      query === '' ? stream.lines : stream.lines.filter((l) => l.marker || matches(l.text, query, regex));
    return filterLines(searched, filter);
  }, [stream.lines, query, regex, filter]);
  const search = useMemo(() => (query === '' ? undefined : { query, regex }), [query, regex]);

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

  const download = async () => {
    setExportError(undefined);
    try {
      await exportLogs({
        api,
        saveFile,
        cluster,
        namespace,
        pod,
        container,
        lines: stream.lines,
        format,
        timestamps,
      });
    } catch (e) {
      setExportError(e instanceof Error ? e.message : 'The download failed.');
    }
  };

  return (
    <div className="log-viewer">
      <div className="log-controls">
        <MultiSourcePicker pod={pod} containers={containers} value={source} onChange={setSource} />
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
        <label>
          Download as
          <select value={format} onChange={(e) => setFormat(e.target.value as ExportFormat)}>
            <option value="text">Text</option>
            <option value="ndjson">NDJSON</option>
          </select>
        </label>
        <Button onClick={() => void download()}>Download</Button>
      </div>
      <FieldFilter value={filter} onChange={setFilter} />
      {exportError && <p role="alert">{exportError}</p>}
      {stream.status === 'connecting' && <p role="status">Connecting…</p>}
      {stream.status === 'ended' && <p role="status">The log ended.</p>}
      {stream.status === 'error' && <p role="alert">{stream.error}</p>}
      {stream.notice && <p role="status">{stream.notice}</p>}
      {stream.historyEnd === 'start' && <p className="log-note">Beginning of the available log</p>}
      {stream.historyEnd === 'cap' && <p className="log-note">No more lines can be loaded.</p>}
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
          if (el.scrollTop <= HISTORY_TRIGGER) stream.loadOlder();
        }}
      >
        <div style={{ height: visible.length * LINE_HEIGHT, position: 'relative' }}>
          {visible.slice(start, end).map((l, offset) => {
            const index = start + offset;
            const label = sourceLabel(l);
            return (
              <div
                key={index}
                className={l.marker ? 'log-line log-marker' : 'log-line'}
                style={{ position: 'absolute', top: index * LINE_HEIGHT, height: LINE_HEIGHT }}
              >
                {label !== undefined && (
                  <span className={`log-source source-${sourceColor(l.pod ?? '', l.container ?? '')}`}>
                    {label}
                  </span>
                )}
                {timestamps && !l.marker && <span className="log-ts">{l.ts} </span>}
                <LineBody line={l} search={search} />
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

/** A line's text: JSON lines show their level as a label and their message, other lines are tokenised. */
function LineBody({
  line,
  search,
}: {
  line: LogLine;
  search: { query: string; regex: boolean } | undefined;
}) {
  if (line.marker) return <span>{line.text}</span>;
  const p = parsed(line);
  if (!p.json) return <LogText text={line.text} highlight={search} />;
  const rest = otherFields(p.fields);
  return (
    <>
      {p.level && <span className={`log-level ${levelClass(p.level)}`}>{levelLabel(p.level)}</span>}{' '}
      <LogText text={p.message ?? (rest === '' ? line.text : '')} highlight={search} />
      {rest !== '' && <span className="log-fields"> {rest}</span>}
    </>
  );
}
