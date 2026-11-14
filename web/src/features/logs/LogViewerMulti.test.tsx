import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { type ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { ApiContext } from '../../api/context';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { SocketContext } from '../../ws/context';
import { createSocket, type WebSocketCtor } from '../../ws/socket';
import { LogViewer } from './LogViewer';

function makeSocket() {
  FakeWebSocket.reset();
  return createSocket({
    url: 'ws://x/api/v1/ws',
    WebSocketCtor: FakeWebSocket as unknown as WebSocketCtor,
    timers: { setTimeout: (fn, ms) => setTimeout(fn, ms), clearTimeout: (id) => clearTimeout(id as number) },
    visibility: { isHidden: () => false, subscribe: () => () => undefined },
  });
}

function wrap(
  node: ReactNode,
  fetch: (url: string, init?: RequestInit) => Promise<Response> = () =>
    Promise.resolve(new Response(null, { status: 204 })),
) {
  const socket = makeSocket();
  const client = createClient({ fetch });
  const result = render(
    <ApiContext.Provider value={client}>
      <SocketContext.Provider value={socket}>{node}</SocketContext.Provider>
    </ApiContext.Provider>,
  );
  act(() => FakeWebSocket.last.serverOpen());
  return result;
}

const ln = (n: number, text = `line ${n}`, pod = 'web-1', container = 'app') => ({
  ts: `2026-11-14T12:00:${String(n).padStart(2, '0')}Z`,
  text,
  pod,
  container,
});
const send = (type: string, d: unknown, ch = 1) =>
  act(() => FakeWebSocket.last.serverSend({ ch, t: type, d }));
const frames = (op: string) => FakeWebSocket.instances.flatMap((w) => w.frames).filter((f) => f.op === op);

const props = {
  cluster: 'v:prod',
  namespace: 'ns',
  pod: 'web-1',
  containers: ['app', 'sidecar'],
  maxLines: 50,
  viewportHeight: 180,
};

async function followAll() {
  await userEvent.selectOptions(screen.getByLabelText('Source'), 'all');
}

describe('LogViewer: several containers', () => {
  beforeEach(() => FakeWebSocket.reset());

  it('all containers re-opens the channel with sources and labels every line with its source', async () => {
    wrap(<LogViewer {...props} />);
    await followAll();
    expect(frames('logs.open').at(-1)?.d).toMatchObject({
      sources: { pod: 'web-1', allContainers: true },
      follow: true,
    });
    expect(frames('logs.open').at(-1)?.d).not.toHaveProperty('container');
    send('lines', {
      lines: [ln(1, 'from app'), ln(2, 'from sidecar', 'web-1', 'sidecar'), ln(3, 'app again')],
    });
    const log = screen.getByRole('log');
    const labels = [...log.querySelectorAll('.log-source')].map((e) => e.textContent);
    expect(labels).toEqual(['web-1/app', 'web-1/sidecar', 'web-1/app']);
    const classes = [...log.querySelectorAll('.log-source')].map((e) => e.className);
    expect(classes[0]).toBe(classes[2]);
    expect(classes[0]).not.toBe(classes[1]);
    expect(classes[0]).toMatch(/source-\d+/);
  });

  it('one container needs no label', () => {
    wrap(<LogViewer {...props} />);
    send('lines', { lines: [{ ts: '2026-11-14T12:00:01Z', text: 'x' }] });
    expect(screen.getByRole('log').querySelector('.log-source')).toBeNull();
  });

  it('a label selector or a workload follows several pods', async () => {
    wrap(<LogViewer {...props} />);
    await userEvent.selectOptions(screen.getByLabelText('Source'), 'selector');
    await userEvent.type(screen.getByLabelText('Label selector'), 'app=web');
    await userEvent.click(screen.getByRole('button', { name: 'Follow' }));
    expect(frames('logs.open').at(-1)?.d).toMatchObject({ sources: { selector: 'app=web' } });
  });

  it('a source that cannot be opened is shown without ending the others', async () => {
    wrap(<LogViewer {...props} />);
    await followAll();
    send('opened', undefined);
    send('source_error', {
      pod: 'web-1',
      container: 'sidecar',
      message: 'container sidecar is waiting to start',
    });
    expect(screen.getByRole('status')).toHaveTextContent(
      'web-1/sidecar: container sidecar is waiting to start',
    );
    send('lines', { lines: [ln(1)] });
    expect(screen.getByText('line 1')).toBeInTheDocument();
  });
});

describe('LogViewer: structure', () => {
  beforeEach(() => FakeWebSocket.reset());

  const jsonLine = (n: number, o: Record<string, unknown>) => ({
    ts: `2026-11-14T12:00:0${n}Z`,
    text: JSON.stringify(o),
  });

  it('shows the level as coloured text and the message of a JSON line', () => {
    wrap(<LogViewer {...props} />);
    send('lines', { lines: [jsonLine(1, { level: 'error', msg: 'db down', retries: 3 })] });
    const log = screen.getByRole('log');
    expect(log.querySelector('.level-error')).toHaveTextContent('ERROR');
    expect(within(log).getByText('db down')).toBeInTheDocument();
    expect(within(log).getByText(/retries/)).toBeInTheDocument();
  });

  it('filters by level and by field value; plain lines stay unless a field filter is on', async () => {
    wrap(<LogViewer {...props} />);
    send('lines', {
      lines: [
        jsonLine(1, { level: 'info', msg: 'ok one', user: 'ada' }),
        jsonLine(2, { level: 'error', msg: 'bad two', user: 'bob' }),
        { ts: '2026-11-14T12:00:03Z', text: 'plain three' },
      ],
    });
    const log = screen.getByRole('log');
    await userEvent.click(screen.getByRole('checkbox', { name: 'INFO' }));
    expect(within(log).queryByText('ok one')).toBeNull();
    expect(within(log).getByText('bad two')).toBeInTheDocument();
    expect(within(log).getByText('plain three')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('checkbox', { name: 'INFO' }));
    await userEvent.type(screen.getByLabelText('Field'), 'user');
    await userEvent.type(screen.getByLabelText('Value'), 'ada');
    expect(within(log).getByText('ok one')).toBeInTheDocument();
    expect(within(log).queryByText('bad two')).toBeNull();
    expect(within(log).queryByText('plain three')).toBeNull();
  });

  it('turns URLs into links and never interprets the text as HTML', () => {
    wrap(<LogViewer {...props} />);
    send('lines', { lines: [{ ts: '2026-11-14T12:00:01Z', text: 'GET https://example.com/a <b>x</b>' }] });
    const log = screen.getByRole('log');
    expect(within(log).getByRole('link', { name: 'https://example.com/a' })).toBeInTheDocument();
    expect(log.querySelector('b')).toBeNull();
  });
});

describe('LogViewer: history', () => {
  beforeEach(() => FakeWebSocket.reset());

  function scrollToTop() {
    fireEvent.scroll(screen.getByRole('log'), { target: { scrollTop: 0 } });
  }

  it('scrolling to the top asks for older lines once, and prepends the answer', async () => {
    wrap(<LogViewer {...props} />);
    send('opened', undefined);
    send('lines', {
      lines: Array.from({ length: 30 }, (_, i) => ({ ...ln(i + 10), pod: undefined, container: undefined })),
    });
    await userEvent.click(screen.getByRole('checkbox', { name: 'Follow' }));
    scrollToTop();
    scrollToTop(); // a second scroll event while the first is pending asks for nothing
    const asks = frames('logs.history');
    expect(asks).toHaveLength(1);
    expect(asks[0]).toMatchObject({
      ch: 1,
      d: { source: { pod: 'web-1', container: 'app' }, before: '2026-11-14T12:00:10Z', lines: 500 },
    });
    send('history', { pod: 'web-1', container: 'app', lines: [ln(1, 'older one'), ln(2, 'older two')] });
    await waitFor(() => expect(screen.getByRole('log').textContent).toContain('older one'));
    scrollToTop();
    expect(frames('logs.history')).toHaveLength(2);
    expect(frames('logs.history')[1]?.d).toMatchObject({ before: '2026-11-14T12:00:01Z' });
  });

  it('at the end of the log it says so and stops asking', async () => {
    wrap(<LogViewer {...props} />);
    send('lines', { lines: [ln(5)] });
    await userEvent.click(screen.getByRole('checkbox', { name: 'Follow' }));
    scrollToTop();
    send('historyEnd', { pod: 'web-1', container: 'app', reason: 'start' });
    expect(screen.getByText('Beginning of the available log')).toBeInTheDocument();
    scrollToTop();
    expect(frames('logs.history')).toHaveLength(1);
  });

  it('a cap says why no more can be loaded', async () => {
    wrap(<LogViewer {...props} />);
    send('lines', { lines: [ln(5)] });
    await userEvent.click(screen.getByRole('checkbox', { name: 'Follow' }));
    scrollToTop();
    send('historyEnd', { pod: 'web-1', container: 'app', reason: 'cap' });
    expect(screen.getByText('No more lines can be loaded.')).toBeInTheDocument();
  });

  it('asks for the oldest line of each source separately', async () => {
    wrap(<LogViewer {...props} />);
    await followAll();
    send('lines', { lines: [ln(5, 'a', 'web-1', 'app'), ln(3, 'b', 'web-1', 'sidecar')] });
    await userEvent.click(screen.getByRole('checkbox', { name: 'Follow' }));
    scrollToTop();
    const asked = frames('logs.history').map((f) => f.d);
    expect(asked).toEqual([
      { source: { pod: 'web-1', container: 'app' }, before: '2026-11-14T12:00:05Z', lines: 500 },
      { source: { pod: 'web-1', container: 'sidecar' }, before: '2026-11-14T12:00:03Z', lines: 500 },
    ]);
  });
});

describe('LogViewer: export', () => {
  beforeEach(() => FakeWebSocket.reset());

  it('saves after the server recorded the download', async () => {
    const calls: string[] = [];
    const saveFile = vi.fn((name: string) => calls.push(`save ${name}`));
    wrap(<LogViewer {...props} saveFile={saveFile} />, (url, init) => {
      calls.push(`${init?.method} ${url}`);
      return Promise.resolve(new Response(null, { status: 204 }));
    });
    send('lines', { lines: [{ ts: '2026-11-14T12:00:01Z', text: 'x' }] });
    await userEvent.selectOptions(screen.getByLabelText('Download as'), 'ndjson');
    await userEvent.click(screen.getByRole('button', { name: 'Download' }));
    await waitFor(() => expect(saveFile).toHaveBeenCalled());
    expect(calls).toEqual(['POST /api/v1/clusters/v%3Aprod/logs/downloads', 'save web-1-app.ndjson']);
  });

  it('saves nothing and shows the error when the server refuses', async () => {
    const saveFile = vi.fn();
    wrap(<LogViewer {...props} saveFile={saveFile} />, () =>
      Promise.resolve(
        new Response(JSON.stringify({ error: { code: 'server_busy', message: 'Audit unavailable.' } }), {
          status: 503,
        }),
      ),
    );
    send('lines', { lines: [{ ts: '2026-11-14T12:00:01Z', text: 'x' }] });
    await userEvent.click(screen.getByRole('button', { name: 'Download' }));
    expect(await screen.findByText('Audit unavailable.')).toBeInTheDocument();
    expect(saveFile).not.toHaveBeenCalled();
  });
});
