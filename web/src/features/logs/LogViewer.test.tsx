import { act, fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { type ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { SocketContext } from '../../ws/context';
import { createSocket, type WebSocketCtor } from '../../ws/socket';
import { LogViewer, browserSaveFile } from './LogViewer';
import { useLogStream } from './useLogStream';

function makeSocket() {
  FakeWebSocket.reset();
  return createSocket({
    url: 'ws://x/api/v1/ws',
    WebSocketCtor: FakeWebSocket as unknown as WebSocketCtor,
    timers: { setTimeout: (fn, ms) => setTimeout(fn, ms), clearTimeout: (id) => clearTimeout(id as number) },
    visibility: { isHidden: () => false, subscribe: () => () => undefined },
  });
}

function wrap(node: ReactNode) {
  const socket = makeSocket();
  const result = render(<SocketContext.Provider value={socket}>{node}</SocketContext.Provider>);
  act(() => FakeWebSocket.last.serverOpen());
  return { ...result, socket };
}

const ln = (n: number, text = `line ${n}`) => ({
  ts: `2026-11-14T12:00:${String(n).padStart(2, '0')}Z`,
  text,
});
const send = (type: string, d: unknown, ch = 1) =>
  act(() => FakeWebSocket.last.serverSend({ ch, t: type, d }));
const opens = () => FakeWebSocket.instances.flatMap((w) => w.frames).filter((f) => f.op === 'logs.open');

const props = {
  cluster: 'v:prod',
  namespace: 'ns',
  pod: 'web-1',
  containers: ['app', 'sidecar'],
  maxLines: 5,
  viewportHeight: 180,
};

describe('useLogStream', () => {
  beforeEach(() => FakeWebSocket.reset());

  function Probe({ request }: { request: Parameters<typeof useLogStream>[0] }) {
    const s = useLogStream(request, 3);
    return (
      <div>
        <p data-testid="status">{s.status}</p>
        <p data-testid="lines">{s.lines.map((l) => l.text).join('|')}</p>
        <button onClick={s.paused ? s.resume : s.pause}>{s.paused ? 'resume' : 'pause'}</button>
      </div>
    );
  }
  const request = { cluster: 'c', namespace: 'ns', pod: 'p', container: 'app', tailLines: 100 };

  it('opens logs.open with the request and follows', () => {
    wrap(<Probe request={request} />);
    expect(opens()[0]).toMatchObject({
      op: 'logs.open',
      ch: 1,
      d: { cluster: 'c', namespace: 'ns', pod: 'p', container: 'app', follow: true, tailLines: 100 },
    });
    expect(screen.getByTestId('status')).toHaveTextContent('connecting');
    send('opened', undefined);
    expect(screen.getByTestId('status')).toHaveTextContent('streaming');
  });

  it('keeps at most the buffer size and shows dropped markers', () => {
    wrap(<Probe request={request} />);
    send('lines', { lines: [ln(1), ln(2), ln(3), ln(4)] });
    expect(screen.getByTestId('lines')).toHaveTextContent('line 2|line 3|line 4');
    send('dropped', { count: 7 });
    expect(screen.getByTestId('lines')).toHaveTextContent('line 4|… 7 lines dropped …');
  });

  it('pausing freezes the view but keeps buffering; resuming shows everything', async () => {
    wrap(<Probe request={request} />);
    send('lines', { lines: [ln(1)] });
    await userEvent.click(screen.getByRole('button', { name: 'pause' }));
    send('lines', { lines: [ln(2), ln(3)] });
    expect(screen.getByTestId('lines')).toHaveTextContent(/^line 1$/);
    await userEvent.click(screen.getByRole('button', { name: 'resume' }));
    expect(screen.getByTestId('lines')).toHaveTextContent('line 1|line 2|line 3');
  });

  it('ends on eof and fails on an error frame', () => {
    wrap(<Probe request={request} />);
    send('eof', {});
    expect(screen.getByTestId('status')).toHaveTextContent('ended');
    send('error', { message: 'pod not found' });
    expect(screen.getByTestId('status')).toHaveTextContent('error');
  });

  it('re-opens after a reconnect with sinceTime = the last timestamp and skips lines it already has', () => {
    vi.useFakeTimers();
    wrap(<Probe request={request} />);
    send('opened', undefined);
    send('lines', { lines: [ln(1), ln(2)] });
    act(() => FakeWebSocket.last.serverClose(1006));
    act(() => void vi.advanceTimersByTime(1000));
    act(() => FakeWebSocket.last.serverOpen());
    expect(opens()[1]?.d).toMatchObject({ sinceTime: ln(2).ts });
    expect(opens()[1]?.d).not.toHaveProperty('tailLines');
    send('opened', undefined);
    send('lines', { lines: [ln(1), ln(2), ln(3)] }); // the server repeats what the client already has
    expect(screen.getByTestId('lines')).toHaveTextContent('line 1|line 2|line 3');
    vi.useRealTimers();
  });

  it('asks for the previous instance without following; an empty request does nothing', () => {
    wrap(<Probe request={{ ...request, previous: true, sinceSeconds: 60 }} />);
    expect(opens()[0]?.d).toMatchObject({ follow: false, previous: true, sinceSeconds: 60 });
  });

  it('opens nothing without a request', () => {
    const socket = makeSocket();
    render(
      <SocketContext.Provider value={socket}>
        <Probe request={undefined} />
      </SocketContext.Provider>,
    );
    expect(FakeWebSocket.instances).toHaveLength(0);
  });
});

describe('LogViewer', () => {
  beforeEach(() => FakeWebSocket.reset());

  it('renders lines as text with timestamps, and can hide the timestamps', async () => {
    wrap(<LogViewer {...props} />);
    send('lines', { lines: [ln(1, '<script>alert(1)</script>')] });
    const log = screen.getByRole('log', { name: 'Logs of web-1' });
    expect(within(log).getByText('<script>alert(1)</script>')).toBeInTheDocument();
    expect(log.querySelector('script')).toBeNull();
    expect(within(log).getByText(/2026-11-14T12:00:01Z/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('checkbox', { name: 'Timestamps' }));
    expect(within(log).queryByText(/2026-11-14T12:00:01Z/)).toBeNull();
  });

  it('switching the container, tail, since or previous re-opens the stream', async () => {
    wrap(<LogViewer {...props} />);
    await userEvent.selectOptions(screen.getByLabelText('Container'), 'sidecar');
    await userEvent.selectOptions(screen.getByLabelText('Tail'), '1000');
    await userEvent.selectOptions(screen.getByLabelText('Since'), 'Last hour');
    await userEvent.click(screen.getByRole('checkbox', { name: 'Previous instance' }));
    const last = opens().at(-1);
    expect(last?.d).toMatchObject({
      container: 'sidecar',
      tailLines: 1000,
      sinceSeconds: 3600,
      previous: true,
      follow: false,
    });
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'logs.close').length).toBeGreaterThanOrEqual(3);
  });

  it('searches on the client, highlighting matches with <mark>', async () => {
    wrap(<LogViewer {...props} />);
    send('lines', { lines: [ln(1, 'all fine'), ln(2, 'an ERROR happened'), ln(3, 'another error')] });
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search' }), 'error');
    const log = screen.getByRole('log');
    expect(within(log).queryByText('all fine')).toBeNull();
    const marks = log.querySelectorAll('mark');
    expect([...marks].map((m) => m.textContent)).toEqual(['ERROR', 'error']);
  });

  it('supports a regular expression; an invalid one shows an error and filters nothing', async () => {
    wrap(<LogViewer {...props} />);
    send('lines', { lines: [ln(1, 'id=42'), ln(2, 'plain')] });
    await userEvent.click(screen.getByRole('checkbox', { name: 'Regex' }));
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search' }), 'id=\\d+');
    expect(screen.queryByText('plain')).toBeNull();
    await userEvent.clear(screen.getByRole('searchbox', { name: 'Search' }));
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search' }), '(');
    expect(screen.getByRole('alert')).toHaveTextContent('Invalid regular expression');
    expect(screen.getByText('plain')).toBeInTheDocument();
    expect(screen.getByText('id=42')).toBeInTheDocument();
  });

  it('shows the dropped marker, the status messages and pauses', async () => {
    wrap(<LogViewer {...props} />);
    expect(screen.getByRole('status')).toHaveTextContent('Connecting');
    send('opened', undefined);
    send('lines', { lines: [ln(1)] });
    send('dropped', { count: 12 });
    expect(screen.getByText('… 12 lines dropped …')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Pause' }));
    send('lines', { lines: [ln(2, 'hidden while paused')] });
    expect(screen.queryByText('hidden while paused')).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: 'Resume' }));
    expect(screen.getByText('hidden while paused')).toBeInTheDocument();
    send('eof', {});
    expect(screen.getByRole('status')).toHaveTextContent('The log ended.');
  });

  it('shows an error from the server', () => {
    wrap(<LogViewer {...props} />);
    send('error', { code: 'not_found', message: 'pods "web-1" not found' });
    expect(screen.getByRole('alert')).toHaveTextContent('pods "web-1" not found');
  });

  it('downloads the buffered lines through the injected saveFile', async () => {
    const saveFile = vi.fn();
    wrap(<LogViewer {...props} saveFile={saveFile} />);
    send('lines', { lines: [ln(1), ln(2)] });
    send('dropped', { count: 3 });
    await userEvent.click(screen.getByRole('button', { name: 'Download' }));
    expect(saveFile).toHaveBeenCalledTimes(1);
    const [name, blob] = saveFile.mock.calls[0] as [string, Blob];
    expect(name).toBe('web-1-app.log');
    const text = await new Promise<string>((resolve) => {
      const r = new FileReader();
      r.onload = () => resolve(String(r.result));
      r.readAsText(blob);
    });
    expect(text).toBe('2026-11-14T12:00:01Z line 1\n2026-11-14T12:00:02Z line 2\n');
  });

  it('follows the tail while at the bottom and lets go when the reader scrolls up', () => {
    wrap(<LogViewer {...props} maxLines={1000} />);
    const log = screen.getByRole('log');
    Object.defineProperty(log, 'scrollHeight', { configurable: true, value: 18 * 100 });
    send('lines', { lines: Array.from({ length: 100 }, (_, i) => ln(i % 60, `line ${i}`)) });
    expect(log.scrollTop).toBe(18 * 100);

    log.scrollTop = 0;
    fireEvent.scroll(log);
    send('lines', { lines: [ln(1, 'more')] });
    expect(log.scrollTop).toBe(0);

    log.scrollTop = 18 * 101 - 180;
    fireEvent.scroll(log);
    send('lines', { lines: [ln(2, 'again')] });
    expect(log.scrollTop).toBe(18 * 102);
  });

  it('only renders the lines in view', () => {
    wrap(<LogViewer {...props} maxLines={10_000} />);
    send('lines', { lines: Array.from({ length: 5000 }, (_, i) => ln(i % 60, `row ${i}`)) });
    const log = screen.getByRole('log');
    expect(log.querySelectorAll('.log-line').length).toBeLessThan(60);
  });

  it('does not follow when Follow is off', async () => {
    wrap(<LogViewer {...props} maxLines={1000} />);
    await userEvent.click(screen.getByRole('checkbox', { name: 'Follow' }));
    const log = screen.getByRole('log');
    send('lines', { lines: Array.from({ length: 100 }, (_, i) => ln(i % 60)) });
    expect(log.scrollTop).toBe(0);
  });

  it('closes its channel when it goes away', () => {
    const { unmount } = wrap(<LogViewer {...props} />);
    unmount();
    expect(FakeWebSocket.last.frames.some((f) => f.op === 'logs.close' && f.ch === 1)).toBe(true);
  });
});

describe('browserSaveFile', () => {
  it('clicks a temporary download link', () => {
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined);
    URL.createObjectURL = vi.fn(() => 'blob:x');
    URL.revokeObjectURL = vi.fn();
    browserSaveFile('a.log', new Blob(['x']));
    expect(click).toHaveBeenCalled();
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:x');
  });
});
