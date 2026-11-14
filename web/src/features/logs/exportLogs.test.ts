import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse } from '../../test/render';
import { exportLogs, logFileName, toNdjson, toText } from './exportLogs';
import type { LogLine } from './logBuffer';

const lines: LogLine[] = [
  { ts: '2026-11-14T12:00:00Z', text: 'first', pod: 'web-1', container: 'app' },
  { ts: '', text: '… 2 lines dropped …', marker: true },
  { ts: '2026-11-14T12:00:01Z', text: 'second "quoted"', pod: 'web-1', container: 'app' },
];

describe('formats', () => {
  it('text has the timestamp first and no markers', () => {
    expect(toText(lines, true)).toBe('2026-11-14T12:00:00Z first\n2026-11-14T12:00:01Z second "quoted"\n');
    expect(toText(lines, false)).toBe('first\nsecond "quoted"\n');
    expect(toText([], true)).toBe('');
  });

  it('NDJSON is one object per line with time, pod, container and message', () => {
    const out = toNdjson(lines, 'fallback-pod', 'fallback-container');
    const rows = out
      .trimEnd()
      .split('\n')
      .map((l) => JSON.parse(l) as unknown);
    expect(rows).toEqual([
      { time: '2026-11-14T12:00:00Z', pod: 'web-1', container: 'app', message: 'first' },
      { time: '2026-11-14T12:00:01Z', pod: 'web-1', container: 'app', message: 'second "quoted"' },
    ]);
    expect(out.endsWith('\n')).toBe(true);
  });

  it("NDJSON falls back to the viewer's own pod and container for untagged lines", () => {
    const out = toNdjson([{ ts: 't', text: 'x' }], 'p', 'c');
    expect(JSON.parse(out)).toEqual({ time: 't', pod: 'p', container: 'c', message: 'x' });
  });

  it('names the file after the pod and container', () => {
    expect(logFileName('web-1', 'app', 'text')).toBe('web-1-app.log');
    expect(logFileName('web-1', 'app', 'ndjson')).toBe('web-1-app.ndjson');
    expect(logFileName('web-1', '', 'text')).toBe('web-1.log');
    expect(logFileName('a/b', 'c d', 'text')).toBe('a_b-c_d.log');
  });
});

describe('exportLogs', () => {
  const base = {
    cluster: 'v:prod',
    namespace: 'ns',
    pod: 'web-1',
    container: 'app',
    lines,
    timestamps: true,
  };

  it('records the download on the server first and saves only after it succeeded', async () => {
    const order: string[] = [];
    const fetch = vi.fn((url: string, init?: RequestInit) => {
      order.push(`${init?.method} ${url} ${String(init?.body)}`);
      return Promise.resolve(new Response(null, { status: 204 }));
    });
    const saveFile = vi.fn((name: string) => order.push(`save ${name}`));
    await exportLogs({ ...base, format: 'ndjson', api: createClient({ fetch }), saveFile });
    expect(order).toHaveLength(2);
    expect(order[0]).toContain('POST /api/v1/clusters/v%3Aprod/logs/downloads');
    expect(JSON.parse(order[0]?.split(' ').slice(2).join(' ') ?? '{}')).toEqual({
      namespace: 'ns',
      pod: 'web-1',
      container: 'app',
      format: 'ndjson',
      from: '2026-11-14T12:00:00Z',
      to: '2026-11-14T12:00:01Z',
      lines: 2,
    });
    expect(order[1]).toBe('save web-1-app.ndjson');
  });

  it('saves nothing when the server refuses, and says why', async () => {
    const fetch = vi.fn(() =>
      Promise.resolve(jsonResponse({ error: { code: 'server_busy', message: 'Audit unavailable.' } }, 503)),
    );
    const saveFile = vi.fn();
    await expect(
      exportLogs({ ...base, format: 'text', api: createClient({ fetch }), saveFile }),
    ).rejects.toThrow('Audit unavailable.');
    expect(saveFile).not.toHaveBeenCalled();
  });

  it('saves a text file with the right content and type', async () => {
    const fetch = vi.fn(() => Promise.resolve(new Response(null, { status: 204 })));
    const saved: Array<{ name: string; blob: Blob }> = [];
    await exportLogs({
      ...base,
      format: 'text',
      api: createClient({ fetch }),
      saveFile: (name, blob) => saved.push({ name, blob }),
    });
    expect(saved[0]?.name).toBe('web-1-app.log');
    expect(await saved[0]?.blob.text()).toBe(
      '2026-11-14T12:00:00Z first\n2026-11-14T12:00:01Z second "quoted"\n',
    );
    expect(saved[0]?.blob.type).toBe('text/plain');
  });

  it('exporting nothing is refused', async () => {
    const fetch = vi.fn();
    await expect(
      exportLogs({ ...base, lines: [], format: 'text', api: createClient({ fetch }), saveFile: vi.fn() }),
    ).rejects.toThrow('There are no lines to export.');
    expect(fetch).not.toHaveBeenCalled();
  });
});
