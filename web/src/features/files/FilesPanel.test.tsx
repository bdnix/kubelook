import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { encodeUtf8 } from '../../lib/bytes';
import { jsonResponse, renderApp } from '../../test/render';
import type { SaveFile } from '../../lib/saveFile';
import { FilesPanel } from './FilesPanel';
import { FileError, type Files, type FileEntry } from './useFiles';

vi.mock('../../adapters/monaco/CodeEditor', () => ({
  CodeEditor: (p: { value: string; onChange: (v: string) => void; readOnly?: boolean; label: string }) => (
    <textarea
      aria-label={p.label}
      value={p.value}
      readOnly={p.readOnly}
      onChange={(e) => p.onChange(e.target.value)}
    />
  ),
}));

let files: Files;
vi.mock('./useFiles', async (orig) => ({
  ...(await orig<typeof import('./useFiles')>()),
  useFiles: () => files,
}));

const entry = (
  name: string,
  type: FileEntry['type'] = 'file',
  extra: Partial<FileEntry> = {},
): FileEntry => ({
  name,
  type,
  mode: type === 'dir' ? 'drwxr-xr-x' : '-rw-r--r--',
  size: 10,
  modified: '2026-11-14T10:00:00Z',
  ...extra,
});

const stat = { size: 5, modified: '2026-11-14T10:00:00Z' };

function makeFiles(over: Partial<Files> = {}): Files {
  return {
    status: 'ready',
    list: vi.fn(() =>
      Promise.resolve({
        entries: [entry('etc', 'dir'), entry('hello.txt'), entry('current', 'link', { target: 'etc' })],
        skipped: 0,
      }),
    ),
    read: vi.fn(() => Promise.resolve({ data: encodeUtf8('hello'), binary: false, truncated: false, stat })),
    save: vi.fn(() => Promise.resolve({ size: 6, modified: '2026-11-14T11:00:00Z' })),
    remove: vi.fn(() => Promise.resolve()),
    download: vi.fn(() => Promise.resolve({ data: encodeUtf8('payload'), tar: false })),
    upload: vi.fn(() => Promise.resolve()),
    reconnect: vi.fn(),
    ...over,
  };
}

const target = { cluster: 'v:prod', namespace: 'ns', pod: 'web-1', container: 'app' };

const meBody = (fileBrowser: boolean) => ({
  user: { id: 'u', provider: 'google', email: 'a@b', name: 'A' },
  csrfToken: 'c',
  settings: { favouriteClusters: null, watchIdleSeconds: 30, theme: 'light' },
  maintenanceBanner: '',
  features: { kubeconfig: true, fileBrowser, nodeShell: false, vault: true, helmVersion: 4, modules: {} },
  limits: {
    fileTransferBytes: 1,
    logsHistoryMaxLines: 1,
    logsLiveBufferLines: 1,
    vaultExpiryWarningSeconds: 1,
  },
  vault: null,
  keyId: 'k',
});

function mount(opts: { fileBrowser?: boolean; allowed?: boolean; saveFile?: SaveFile } = {}) {
  const fetch = vi.fn((url: string) =>
    Promise.resolve(
      url.endsWith('/me')
        ? jsonResponse(meBody(opts.fileBrowser ?? true))
        : jsonResponse({ results: [{ allowed: opts.allowed ?? true }] }),
    ),
  );
  const saveFile = opts.saveFile ?? vi.fn<SaveFile>();
  renderApp(<FilesPanel target={target} saveFile={saveFile} />, { client: createClient({ fetch }) });
  return { saveFile, fetch };
}

beforeEach(() => {
  files = makeFiles();
});

describe('FilesPanel', () => {
  it('is hidden when the feature is off or the user may not exec in the pod', async () => {
    const off = mount({ fileBrowser: false });
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByRole('table', { name: 'Files' })).toBeNull();
    expect(off.fetch).toHaveBeenCalled();
  });

  it('is hidden without exec permission', async () => {
    mount({ allowed: false });
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByRole('table', { name: 'Files' })).toBeNull();
  });

  it('lists the root first and navigates into directories and with the breadcrumbs', async () => {
    mount();
    expect(await screen.findByRole('table', { name: 'Files' })).toBeInTheDocument();
    expect(files.list).toHaveBeenCalledWith('/');
    await userEvent.click(screen.getByRole('button', { name: 'etc' }));
    await waitFor(() => expect(files.list).toHaveBeenLastCalledWith('/etc'));
    await userEvent.click(
      within(screen.getByRole('navigation', { name: 'Path' })).getByRole('button', { name: '/' }),
    );
    await waitFor(() => expect(files.list).toHaveBeenLastCalledWith('/'));
  });

  it('opens a file, saves it with the stat it was opened with and shows the new state', async () => {
    mount();
    await userEvent.click(await screen.findByRole('button', { name: 'hello.txt' }));
    const box = await screen.findByRole('textbox', { name: 'Contents of /hello.txt' });
    expect(files.read).toHaveBeenCalledWith('/hello.txt');
    await userEvent.type(box, '!');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(files.save).toHaveBeenCalled());
    const [path, data, expect_] = (files.save as ReturnType<typeof vi.fn>).mock.calls[0] as [
      string,
      Uint8Array,
      unknown,
    ];
    expect(path).toBe('/hello.txt');
    expect(Array.from(data)).toEqual(Array.from(encodeUtf8('hello!')));
    expect(expect_).toEqual(stat);
  });

  it('shows the conflict message when the file changed meanwhile', async () => {
    files = makeFiles({
      save: vi.fn(() =>
        Promise.reject(new FileError('conflict', 'The file changed in the container after you opened it.')),
      ),
    });
    mount();
    await userEvent.click(await screen.findByRole('button', { name: 'hello.txt' }));
    await userEvent.type(await screen.findByRole('textbox'), '!');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The file changed in the container after you opened it.',
    );
  });

  it('binary files show "Binary file" and a download; the editor closes back to the list', async () => {
    files = makeFiles({
      read: vi.fn(() => Promise.resolve({ data: new Uint8Array(), binary: true, truncated: false, stat })),
    });
    const { saveFile } = mount();
    await userEvent.click(await screen.findByRole('button', { name: 'hello.txt' }));
    expect(await screen.findByText('Binary file')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Download' }));
    await waitFor(() => expect(saveFile).toHaveBeenCalledWith('hello.txt', expect.anything()));
    await userEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(await screen.findByRole('table', { name: 'Files' })).toBeInTheDocument();
  });

  it('a link to a directory is entered, a link to a file is opened', async () => {
    mount();
    await userEvent.click(await screen.findByRole('button', { name: 'current' }));
    await waitFor(() => expect(files.list).toHaveBeenLastCalledWith('/etc'));
  });

  it('a link that is not a directory falls back to opening it as a file', async () => {
    files = makeFiles({
      list: vi
        .fn()
        .mockResolvedValueOnce({ entries: [entry('current', 'link', { target: '/etc/hosts' })], skipped: 0 })
        .mockRejectedValueOnce(new FileError('bad_request', 'Not a directory')),
    });
    mount();
    await userEvent.click(await screen.findByRole('button', { name: 'current' }));
    await waitFor(() => expect(files.read).toHaveBeenCalledWith('/current'));
  });

  it('downloads a file, and a directory arrives as a .tar', async () => {
    files = makeFiles({
      download: vi.fn((p: string) => Promise.resolve({ data: encodeUtf8('x'), tar: p === '/etc' })),
    });
    const { saveFile } = mount();
    await userEvent.click(await screen.findByRole('button', { name: 'Download hello.txt' }));
    await waitFor(() => expect(saveFile).toHaveBeenCalledWith('hello.txt', expect.anything()));
    await userEvent.click(screen.getByRole('button', { name: 'Download etc' }));
    await waitFor(() => expect(saveFile).toHaveBeenCalledWith('etc.tar', expect.anything()));
  });

  it('shows download progress while it runs', async () => {
    let progress: (n: number) => void = () => undefined;
    let finish: (v: { data: Uint8Array; tar: boolean }) => void = () => undefined;
    files = makeFiles({
      download: vi.fn((_p: string, onProgress?: (n: number) => void) => {
        progress = onProgress ?? progress;
        return new Promise<{ data: Uint8Array; tar: boolean }>((resolve) => (finish = resolve));
      }),
    });
    mount();
    await userEvent.click(await screen.findByRole('button', { name: 'Download hello.txt' }));
    act(() => progress(1234));
    expect(await screen.findByRole('status')).toHaveTextContent('Downloading hello.txt: 1234 bytes');
    await act(async () => finish({ data: new Uint8Array(), tar: false }));
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull());
  });

  it('delete asks first, naming the path, and then removes it (directories recursively)', async () => {
    mount();
    await userEvent.click(await screen.findByRole('button', { name: 'Delete etc' }));
    const dialog = screen.getByRole('dialog', { name: 'Delete /etc?' });
    expect(dialog).toHaveTextContent('everything in it');
    expect(dialog).toHaveTextContent('kubectl exec -n ns web-1 -c app -- rm -rf -- /etc');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(files.remove).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', { name: 'Delete hello.txt' }));
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(files.remove).toHaveBeenCalledWith('/hello.txt', false));
    await userEvent.click(screen.getByRole('button', { name: 'Delete etc' }));
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(files.remove).toHaveBeenCalledWith('/etc', true));
  });

  it('a failed delete shows its message as text', async () => {
    files = makeFiles({
      remove: vi.fn(() => Promise.reject(new FileError('forbidden', 'rm: Permission denied'))),
    });
    mount();
    await userEvent.click(await screen.findByRole('button', { name: 'Delete hello.txt' }));
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('rm: Permission denied');
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('uploads files one by one and refreshes the list', async () => {
    mount();
    await screen.findByRole('table', { name: 'Files' });
    await userEvent.upload(screen.getByLabelText('Choose files'), [
      new File(['aa'], 'a.txt'),
      new File(['b'], 'b.txt'),
    ]);
    await waitFor(() => expect(files.upload).toHaveBeenCalledTimes(2));
    expect((files.upload as ReturnType<typeof vi.fn>).mock.calls.map((c) => [c[0], c[1]])).toEqual([
      ['/', 'a.txt'],
      ['/', 'b.txt'],
    ]);
    await waitFor(() =>
      expect((files.list as ReturnType<typeof vi.fn>).mock.calls.length).toBeGreaterThan(1),
    );
  });

  it('a folder goes up as one tar archive and progress is shown', async () => {
    let report: (n: number) => void = () => undefined;
    let done: () => void = () => undefined;
    files = makeFiles({
      upload: vi.fn(
        (_d: string, _n: string | undefined, _data: Uint8Array, onProgress?: (n: number) => void) => {
          report = onProgress ?? report;
          return new Promise<void>((resolve) => (done = resolve));
        },
      ),
    });
    mount();
    await screen.findByRole('table', { name: 'Files' });
    const f = new File(['x'], 'main.css');
    Object.defineProperty(f, 'webkitRelativePath', { value: 'site/main.css' });
    const input = screen.getByLabelText('Choose a folder');
    await act(async () => {
      Object.defineProperty(input, 'files', { value: [f], configurable: true });
      input.dispatchEvent(new Event('change', { bubbles: true }));
    });
    await waitFor(() => expect(files.upload).toHaveBeenCalled());
    expect((files.upload as ReturnType<typeof vi.fn>).mock.calls[0]?.[1]).toBeUndefined();
    act(() => report(512));
    expect(await screen.findByRole('status')).toHaveTextContent(/Uploading site\/: \d+%/);
    await act(async () => done());
  });

  it('a path too long for a tar header is reported', async () => {
    mount();
    await screen.findByRole('table', { name: 'Files' });
    const f = new File(['x'], 'x');
    Object.defineProperty(f, 'webkitRelativePath', { value: `dir/${'x'.repeat(130)}` });
    const input = screen.getByLabelText('Choose a folder');
    await act(async () => {
      Object.defineProperty(input, 'files', { value: [f], configurable: true });
      input.dispatchEvent(new Event('change', { bubbles: true }));
    });
    expect(await screen.findByRole('alert')).toHaveTextContent('longer than 100 bytes');
    expect(files.upload).not.toHaveBeenCalled();
  });

  it('shows listing errors as text, and how many lines could not be read', async () => {
    files = makeFiles({
      list: vi.fn().mockRejectedValue(new FileError('not_found', 'ls: No such file or directory')),
    });
    mount();
    expect(await screen.findByRole('alert')).toHaveTextContent('ls: No such file or directory');
  });

  it('unknown failures are reported in general words', async () => {
    files = makeFiles({ list: vi.fn().mockRejectedValue(new TypeError('boom')) });
    mount();
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong.');
  });

  it('says how many listing lines were skipped', async () => {
    files = makeFiles({ list: vi.fn(() => Promise.resolve({ entries: [entry('a')], skipped: 3 })) });
    mount();
    expect(await screen.findByText('3 lines of the listing could not be read.')).toBeInTheDocument();
  });

  it('a lost connection offers a reconnect', async () => {
    files = makeFiles({ status: 'lost' });
    mount();
    await userEvent.click(await screen.findByRole('button', { name: 'Reconnect' }));
    expect(files.reconnect).toHaveBeenCalled();
    expect(files.list).not.toHaveBeenCalled();
  });
});
