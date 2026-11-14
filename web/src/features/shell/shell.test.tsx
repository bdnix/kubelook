import { act, renderHook, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { TerminalHandle } from '../../adapters/xterm/Terminal';
import { createClient } from '../../api/client';
import { bytesToBase64, decodeUtf8, encodeUtf8 } from '../../lib/bytes';
import { dockStore } from '../../state/dock';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { jsonResponse, renderApp } from '../../test/render';
import { createSocket, type WebSocketCtor } from '../../ws/socket';
import { SocketContext } from '../../ws/context';
import { ContainerPicker, runningContainers } from './ContainerPicker';
import { OpenShell } from './OpenShell';
import { ShellTab } from './ShellTab';
import { LOST_MESSAGE, useShell } from './useShell';

vi.mock('../../adapters/xterm/Terminal', () => ({
  Terminal: ({ onReady, label }: { onReady: (h: unknown) => void; label: string }) => {
    onReady(currentHandle);
    return <div role="group" aria-label={label} />;
  },
}));

const request = { cluster: 'v:prod', namespace: 'ns', pod: 'web-1', container: 'app' };

interface FakeTerminal extends TerminalHandle {
  written: Uint8Array[];
  data: (s: string) => void;
  resize: (c: number, r: number) => void;
}

function fakeTerminal(): FakeTerminal {
  let dataCb: (s: string) => void = () => undefined;
  let resizeCb: (c: number, r: number) => void = () => undefined;
  const t: FakeTerminal = {
    written: [],
    write: (b) => void t.written.push(b),
    onData: (cb) => void (dataCb = cb),
    onResize: (cb) => void (resizeCb = cb),
    fit: () => undefined,
    size: () => ({ cols: 100, rows: 30 }),
    data: (s) => dataCb(s),
    resize: (c, r) => resizeCb(c, r),
  };
  return t;
}

let currentHandle: FakeTerminal;

function socketWrapper() {
  FakeWebSocket.reset();
  const socket = createSocket({
    url: 'ws://t/api/v1/ws',
    WebSocketCtor: FakeWebSocket as unknown as WebSocketCtor,
    timers: { setTimeout: () => 0, clearTimeout: () => undefined },
    visibility: { isHidden: () => false, subscribe: () => () => undefined },
  });
  return ({ children }: { children: ReactNode }) => (
    <SocketContext.Provider value={socket}>{children}</SocketContext.Provider>
  );
}

const server = (frame: { t: string; d?: unknown }) =>
  act(() => FakeWebSocket.last.serverSend({ ch: 1, ...frame }));

beforeEach(() => {
  currentHandle = fakeTerminal();
  dockStore.setState({ tabs: [], active: null });
});

describe('useShell', () => {
  function mount() {
    const wrapper = socketWrapper();
    const view = renderHook(() => useShell(request), { wrapper });
    const term = fakeTerminal();
    act(() => view.result.current.attach(term));
    act(() => FakeWebSocket.last.serverOpen());
    return { ...view, term };
  }

  it('waits for the terminal, then opens the channel with the terminal size', () => {
    const wrapper = socketWrapper();
    const { result } = renderHook(() => useShell(request), { wrapper });
    expect(FakeWebSocket.instances).toHaveLength(0);
    act(() => result.current.attach(fakeTerminal()));
    act(() => FakeWebSocket.last.serverOpen());
    expect(FakeWebSocket.last.frames[1]).toEqual({
      op: 'exec.open',
      ch: 1,
      d: { cluster: 'v:prod', namespace: 'ns', pod: 'web-1', container: 'app', cols: 100, rows: 30 },
    });
    expect(result.current.status).toBe('connecting');
  });

  it('opened marks it running; output is written to the terminal as bytes, not strings', () => {
    const { result, term } = mount();
    server({ t: 'opened', d: { shell: '/bin/bash' } });
    expect(result.current).toMatchObject({ status: 'running', shell: '/bin/bash' });
    const bytes = encodeUtf8('héllo ✓');
    server({ t: 'out', d: { data: bytesToBase64(bytes) } });
    expect(Array.from(term.written[0] ?? [])).toEqual(Array.from(bytes));
  });

  it('a multi-byte character split across frames reaches the terminal intact', () => {
    const { term } = mount();
    const all = encodeUtf8('✓');
    server({ t: 'out', d: { data: bytesToBase64(all.subarray(0, 1)) } });
    server({ t: 'out', d: { data: bytesToBase64(all.subarray(1)) } });
    const joined = new Uint8Array([...(term.written[0] ?? []), ...(term.written[1] ?? [])]);
    expect(decodeUtf8(joined)).toBe('✓');
  });

  it('terminal input is sent as base64 stdin and resizes as exec.resize', () => {
    const { term } = mount();
    term.data('ls\r');
    term.resize(120, 40);
    const frames = FakeWebSocket.last.frames;
    expect(frames).toContainEqual({
      op: 'exec.stdin',
      ch: 1,
      d: { data: bytesToBase64(encodeUtf8('ls\r')) },
    });
    expect(frames).toContainEqual({ op: 'exec.resize', ch: 1, d: { cols: 120, rows: 40 } });
  });

  it('exit and closed end the shell with the exit code', () => {
    const { result } = mount();
    server({ t: 'opened', d: { shell: '/bin/sh' } });
    server({ t: 'exit', d: { code: 2 } });
    server({ t: 'closed', d: { reason: 'exited' } });
    expect(result.current).toMatchObject({ status: 'exited', exitCode: 2 });
  });

  it('a close without exit still ends a running shell, e.g. idle', () => {
    const { result } = mount();
    server({ t: 'opened', d: { shell: '/bin/sh' } });
    server({ t: 'closed', d: { reason: 'idle' } });
    expect(result.current.status).toBe('exited');
  });

  it('an error frame shows its message', () => {
    const { result } = mount();
    server({
      t: 'error',
      d: { code: 'no_shell', message: 'This container has no shell (/bin/bash or /bin/sh).' },
    });
    server({ t: 'closed', d: { reason: 'no_shell' } });
    expect(result.current).toMatchObject({
      status: 'error',
      error: 'This container has no shell (/bin/bash or /bin/sh).',
    });
  });

  it('a dropped connection ends the shell with the fixed message and does not reopen it; restart opens a new one', () => {
    const { result, term } = mount();
    server({ t: 'opened', d: { shell: '/bin/bash' } });
    act(() => FakeWebSocket.last.close(1006));
    expect(result.current).toMatchObject({ status: 'lost', error: LOST_MESSAGE });
    term.data('x');
    expect(FakeWebSocket.last.frames.filter((f) => f.op === 'exec.stdin')).toHaveLength(0);
    act(() => result.current.restart());
    expect(FakeWebSocket.instances.length).toBeGreaterThan(0);
    expect(result.current.status).toBe('connecting');
  });

  it('closes the channel when it unmounts', () => {
    const { unmount } = mount();
    unmount();
    expect(FakeWebSocket.last.frames.some((f) => f.op === 'exec.close')).toBe(true);
  });
});

describe('ShellTab', () => {
  it('shows the terminal and a starting message', async () => {
    const Wrapped = socketWrapper();
    renderApp(
      <Wrapped>
        <ShellTab {...request} />
      </Wrapped>,
    );
    expect(await screen.findByRole('group', { name: 'Shell in app' })).toBeInTheDocument();
    expect(screen.getByRole('status')).toHaveTextContent('Starting the shell');
  });

  it('after a lost connection it says so and offers a new shell', async () => {
    const Wrapped = socketWrapper();
    renderApp(
      <Wrapped>
        <ShellTab {...request} />
      </Wrapped>,
    );
    await screen.findByRole('group', { name: 'Shell in app' });
    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
    act(() => FakeWebSocket.last.serverOpen());
    act(() => FakeWebSocket.last.close(1006));
    expect(await screen.findByText(LOST_MESSAGE)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Start a new shell' }));
    expect(await screen.findByRole('status')).toHaveTextContent('Starting the shell');
  });

  it('shows the exit code of a finished shell', async () => {
    const Wrapped = socketWrapper();
    renderApp(
      <Wrapped>
        <ShellTab {...request} />
      </Wrapped>,
    );
    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
    act(() => FakeWebSocket.last.serverOpen());
    server({ t: 'exit', d: { code: 3 } });
    expect(await screen.findByText('The shell ended with exit code 3.')).toBeInTheDocument();
  });

  it('shows an error message from the server', async () => {
    const Wrapped = socketWrapper();
    renderApp(
      <Wrapped>
        <ShellTab {...request} />
      </Wrapped>,
    );
    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
    act(() => FakeWebSocket.last.serverOpen());
    server({ t: 'error', d: { message: 'This container has no shell (/bin/bash or /bin/sh).' } });
    expect(await screen.findByRole('alert')).toHaveTextContent('no shell');
  });
});

describe('runningContainers and ContainerPicker', () => {
  const pod = {
    status: {
      containerStatuses: [
        { name: 'app', state: { running: {} } },
        { name: 'crashed', state: { waiting: { reason: 'CrashLoopBackOff' } } },
      ],
      initContainerStatuses: [
        { name: 'setup', state: { running: {} } },
        { name: 'done', state: { terminated: { exitCode: 0 } } },
      ],
      ephemeralContainerStatuses: [{ name: 'kubelook-debug-abcde', state: { running: {} } }],
    },
  };

  it('lists the running containers, init containers and ephemeral containers only', () => {
    expect(runningContainers(pod)).toEqual([
      { name: 'app', kind: 'container' },
      { name: 'setup', kind: 'init' },
      { name: 'kubelook-debug-abcde', kind: 'ephemeral' },
    ]);
    expect(runningContainers({})).toEqual([]);
  });

  it('labels init and debug containers and reports the choice', async () => {
    const onChange = vi.fn();
    renderApp(<ContainerPicker containers={runningContainers(pod)} value="app" onChange={onChange} />);
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual([
      'app',
      'setup (init)',
      'kubelook-debug-abcde (debug)',
    ]);
    await userEvent.selectOptions(screen.getByLabelText('Container'), 'setup');
    expect(onChange).toHaveBeenCalledWith('setup');
  });
});

describe('OpenShell', () => {
  const pod = {
    metadata: { name: 'web-1', namespace: 'ns' },
    status: {
      containerStatuses: [
        { name: 'app', state: { running: {} } },
        { name: 'side', state: { running: {} } },
      ],
    },
  };
  const client = (allowed: boolean) =>
    createClient({ fetch: () => Promise.resolve(jsonResponse({ results: [{ allowed }] })) });

  it('opens a shell tab in the dock for the chosen container; each open is its own tab', async () => {
    renderApp(<OpenShell cluster="v:prod" pod={pod} />, { client: client(true) });
    await userEvent.selectOptions(screen.getByLabelText('Container'), 'side');
    await userEvent.click(screen.getByRole('button', { name: 'Open shell' }));
    await userEvent.click(screen.getByRole('button', { name: 'Open shell' }));
    const tabs = dockStore.getState().tabs;
    expect(tabs).toHaveLength(2);
    expect(tabs[0]).toMatchObject({
      kind: 'shell',
      title: 'Shell: web-1/side',
      props: { cluster: 'v:prod', namespace: 'ns', pod: 'web-1', container: 'side' },
    });
    expect(tabs[0]?.id).not.toBe(tabs[1]?.id);
  });

  it('is disabled with the missing permission named when the user may not exec', async () => {
    renderApp(<OpenShell cluster="v:prod" pod={pod} />, { client: client(false) });
    const button = screen.getByRole('button', { name: 'Open shell' });
    await waitFor(() => expect(button).toBeDisabled());
    expect(button).toHaveAttribute('title', 'missing permission: create pods/exec');
  });

  it('renders nothing for a pod without a running container', () => {
    renderApp(<OpenShell cluster="c" pod={{ metadata: { name: 'p', namespace: 'n' }, status: {} }} />);
    expect(screen.queryByRole('button', { name: 'Open shell' })).toBeNull();
  });
});
