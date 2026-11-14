import type { ApiClient } from '../../api/client';
import type { LogLine } from './logBuffer';

export type ExportFormat = 'text' | 'ndjson';

const real = (lines: LogLine[]) => lines.filter((l) => !l.marker);

/** One line of text per log line, with the timestamp in front when asked. Markers are not part of the log. */
export function toText(lines: LogLine[], timestamps: boolean): string {
  const out = real(lines).map((l) => (timestamps && l.ts ? `${l.ts} ${l.text}` : l.text));
  return out.length === 0 ? '' : out.join('\n') + '\n';
}

/** One JSON object per line: time, pod, container and message. */
export function toNdjson(lines: LogLine[], pod: string, container: string): string {
  const out = real(lines).map((l) =>
    JSON.stringify({ time: l.ts, pod: l.pod ?? pod, container: l.container ?? container, message: l.text }),
  );
  return out.length === 0 ? '' : out.join('\n') + '\n';
}

const safe = (s: string) => s.replace(/[^A-Za-z0-9._-]/g, '_');

export function logFileName(pod: string, container: string, format: ExportFormat): string {
  const base = container ? `${safe(pod)}-${safe(container)}` : safe(pod);
  return `${base}.${format === 'ndjson' ? 'ndjson' : 'log'}`;
}

export interface ExportOptions {
  api: ApiClient;
  saveFile: (name: string, blob: Blob) => void;
  cluster: string;
  namespace: string;
  pod: string;
  container: string;
  lines: LogLine[];
  format: ExportFormat;
  timestamps: boolean;
}

/**
 * Records the download on the server, then saves the buffer. When the record cannot be written the promise
 * rejects and nothing is saved.
 */
export async function exportLogs(o: ExportOptions): Promise<void> {
  const lines = real(o.lines);
  if (lines.length === 0) throw new Error('There are no lines to export.');
  const times = lines
    .map((l) => l.ts)
    .filter((t) => t !== '')
    .sort((a, b) => Date.parse(a) - Date.parse(b));
  await o.api.post(`/clusters/${encodeURIComponent(o.cluster)}/logs/downloads`, {
    namespace: o.namespace,
    pod: o.pod,
    container: o.container,
    format: o.format,
    from: times[0],
    to: times[times.length - 1],
    lines: lines.length,
  });
  const content = o.format === 'ndjson' ? toNdjson(lines, o.pod, o.container) : toText(lines, o.timestamps);
  o.saveFile(
    logFileName(o.pod, o.container, o.format),
    new Blob([content], { type: o.format === 'ndjson' ? 'application/x-ndjson' : 'text/plain' }),
  );
}
