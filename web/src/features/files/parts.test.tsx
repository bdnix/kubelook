import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { encodeUtf8 } from '../../lib/bytes';
import { renderApp } from '../../test/render';
import { Breadcrumbs } from './Breadcrumbs';
import { FileEditor } from './FileEditor';
import { FileList } from './FileList';
import { UploadDrop } from './UploadDrop';
import type { FileContent, FileEntry } from './useFiles';

vi.mock('../../adapters/monaco/CodeEditor', () => ({
  CodeEditor: (p: {
    value: string;
    onChange: (v: string) => void;
    language: string;
    readOnly?: boolean;
    label: string;
  }) => (
    <textarea
      aria-label={p.label}
      value={p.value}
      readOnly={p.readOnly}
      data-language={p.language}
      onChange={(e) => p.onChange(e.target.value)}
    />
  ),
}));

describe('Breadcrumbs', () => {
  it('navigates to the root and to every directory above', async () => {
    const onNavigate = vi.fn();
    renderApp(<Breadcrumbs path="/var/log/app" onNavigate={onNavigate} />);
    await userEvent.click(screen.getByRole('button', { name: 'log' }));
    await userEvent.click(screen.getByRole('button', { name: '/' }));
    await userEvent.click(screen.getByRole('button', { name: 'app' }));
    expect(onNavigate.mock.calls.map((c) => c[0])).toEqual(['/var/log', '/', '/var/log/app']);
    expect(screen.getByRole('button', { name: 'app' })).toHaveAttribute('aria-current', 'page');
  });

  it('the root is the current page at "/"', () => {
    renderApp(<Breadcrumbs path="/" onNavigate={() => undefined} />);
    expect(screen.getByRole('button', { name: '/' })).toHaveAttribute('aria-current', 'page');
  });
});

const entries: FileEntry[] = [
  { name: 'app.conf', type: 'file', mode: '-rw-r--r--', size: 1536, modified: '2026-11-14T10:00:00Z' },
  { name: 'data dir', type: 'dir', mode: 'drwxr-xr-x', size: 4096, modified: '2025-01-02T00:00:00Z' },
  {
    name: 'current',
    type: 'link',
    mode: 'lrwxrwxrwx',
    size: 11,
    modified: '2026-11-10T08:00:00Z',
    target: 'releases/42',
  },
  { name: 'null', type: 'other', mode: 'crw-rw-rw-', size: 0, modified: '2026-11-14T11:00:00Z' },
];

describe('FileList', () => {
  const props = { onOpen: vi.fn(), onDownload: vi.fn(), onDelete: vi.fn() };

  it('shows a labelled type, mode, human size, modified time and the target of a link', () => {
    renderApp(<FileList entries={entries} {...props} />);
    const row = (name: string) => screen.getByRole('button', { name }).closest('tr') as HTMLElement;
    expect(row('app.conf')).toHaveTextContent('-rw-r--r--');
    expect(row('app.conf')).toHaveTextContent('1.5 KiB');
    expect(row('app.conf').querySelector('[role="img"]')).toHaveAttribute('aria-label', 'File');
    expect(row('data dir').querySelector('[role="img"]')).toHaveAttribute('aria-label', 'Directory');
    expect(row('data dir')).not.toHaveTextContent('4 KiB');
    expect(row('current')).toHaveTextContent('→ releases/42');
    expect(row('current').querySelector('[role="img"]')).toHaveAttribute('aria-label', 'Link');
    expect(row('null').querySelector('[role="img"]')).toHaveAttribute('aria-label', 'Special file');
  });

  it('the filter box narrows the names, and says so when nothing matches', async () => {
    renderApp(<FileList entries={entries} {...props} />);
    await userEvent.type(screen.getByRole('searchbox', { name: 'Filter files' }), 'APP');
    expect(screen.getAllByRole('row')).toHaveLength(2);
    await userEvent.type(screen.getByRole('searchbox', { name: 'Filter files' }), 'zzz');
    expect(screen.getByText('No file matches the filter.')).toBeInTheDocument();
  });

  it('says when the directory is empty', () => {
    renderApp(<FileList entries={[]} {...props} />);
    expect(screen.getByText('This directory is empty.')).toBeInTheDocument();
  });

  it('reports opens, downloads and deletes of the entry', async () => {
    const p = { onOpen: vi.fn(), onDownload: vi.fn(), onDelete: vi.fn() };
    renderApp(<FileList entries={entries} {...p} />);
    await userEvent.click(screen.getByRole('button', { name: 'app.conf' }));
    await userEvent.click(screen.getByRole('button', { name: 'Download app.conf' }));
    await userEvent.click(screen.getByRole('button', { name: 'Delete app.conf' }));
    expect(p.onOpen).toHaveBeenCalledWith(entries[0]);
    expect(p.onDownload).toHaveBeenCalledWith(entries[0]);
    expect(p.onDelete).toHaveBeenCalledWith(entries[0]);
  });
});

const content = (over: Partial<FileContent> = {}): FileContent => ({
  data: encodeUtf8('hello'),
  binary: false,
  truncated: false,
  stat: { size: 5, modified: '2026-11-14T10:00:00Z' },
  ...over,
});

function editor(over: Partial<React.ComponentProps<typeof FileEditor>> = {}) {
  const p = { onSave: vi.fn(), onDownload: vi.fn(), onClose: vi.fn() };
  renderApp(
    <FileEditor path="/etc/app/config.json" content={content()} saving={false} canWrite {...p} {...over} />,
  );
  return p;
}

describe('FileEditor', () => {
  it('loads the text with the language of the extension and saves the changed bytes', async () => {
    const p = editor();
    const box = await screen.findByRole('textbox', { name: 'Contents of /etc/app/config.json' });
    expect(box).toHaveValue('hello');
    expect(box).toHaveAttribute('data-language', 'json');
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
    await userEvent.type(box, '!');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(Array.from(p.onSave.mock.calls[0]?.[0] as Uint8Array)).toEqual(Array.from(encodeUtf8('hello!')));
    expect(screen.getByText('5 B, modified 2026-11-14T10:00:00Z')).toBeInTheDocument();
  });

  it('shows a save error as text', async () => {
    editor({ error: 'The file changed in the container after you opened it.' });
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The file changed in the container after you opened it.',
    );
  });

  it('a binary file offers only a download', () => {
    const p = editor({ content: content({ binary: true, data: new Uint8Array() }) });
    expect(screen.getByText('Binary file')).toBeInTheDocument();
    expect(screen.queryByRole('textbox')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Download' }));
    expect(p.onDownload).toHaveBeenCalled();
  });

  it('bytes that are not UTF-8 text are treated as binary too', () => {
    editor({ content: content({ data: new Uint8Array([0xff, 0xfe]) }) });
    expect(screen.getByText('Binary file')).toBeInTheDocument();
  });

  it('a truncated file is read only with a notice', async () => {
    editor({ content: content({ truncated: true }) });
    expect(screen.getByText(/larger than 1 MiB/)).toBeInTheDocument();
    expect(await screen.findByRole('textbox')).toHaveAttribute('readonly');
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
  });

  it('without write permission there is no Save', async () => {
    editor({ canWrite: false });
    expect(await screen.findByRole('textbox')).toHaveAttribute('readonly');
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
  });

  it('Close calls back', async () => {
    const p = editor();
    await userEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(p.onClose).toHaveBeenCalled();
  });
});

describe('UploadDrop', () => {
  it('picks files with the button input', async () => {
    const onPick = vi.fn();
    renderApp(<UploadDrop onPick={onPick} busy={false} />);
    const input = screen.getByLabelText('Choose files');
    await userEvent.upload(input, [new File(['a'], 'a.txt'), new File(['bb'], 'b.txt')]);
    await waitFor(() => expect(onPick).toHaveBeenCalled());
    const picked = onPick.mock.calls[0]?.[0] as { path: string; data: Uint8Array }[];
    expect(picked.map((f) => f.path)).toEqual(['a.txt', 'b.txt']);
    expect(picked[1]?.data).toHaveLength(2);
  });

  it('a chosen folder keeps its relative paths', async () => {
    const onPick = vi.fn();
    renderApp(<UploadDrop onPick={onPick} busy={false} />);
    const f = new File(['x'], 'main.css');
    Object.defineProperty(f, 'webkitRelativePath', { value: 'site/css/main.css' });
    fireEvent.change(screen.getByLabelText('Choose a folder'), { target: { files: [f] } });
    await waitFor(() => expect(onPick).toHaveBeenCalled());
    expect((onPick.mock.calls[0]?.[0] as { path: string }[])[0]?.path).toBe('site/css/main.css');
  });

  it('files and directories can be dropped', async () => {
    const onPick = vi.fn();
    renderApp(<UploadDrop onPick={onPick} busy={false} />);
    const dropped = {
      isFile: true,
      isDirectory: false,
      name: 'd.txt',
      file: (ok: (f: File) => void) => ok(new File(['dd'], 'd.txt')),
    };
    const zone = screen.getByRole('region', { name: 'Drop files here' });
    fireEvent.dragOver(zone);
    expect(zone).toHaveClass('drop-over');
    fireEvent.drop(zone, {
      dataTransfer: { items: [{ webkitGetAsEntry: () => dropped }, { webkitGetAsEntry: () => null }] },
    });
    await waitFor(() => expect(onPick).toHaveBeenCalled());
    expect((onPick.mock.calls[0]?.[0] as { path: string }[]).map((f) => f.path)).toEqual(['d.txt']);
    expect(zone).not.toHaveClass('drop-over');
    fireEvent.dragOver(zone);
    fireEvent.dragLeave(zone);
    expect(zone).not.toHaveClass('drop-over');
  });

  it('the buttons are disabled while busy', () => {
    renderApp(<UploadDrop onPick={() => undefined} busy />);
    expect(screen.getByRole('button', { name: 'Upload files' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Upload folder' })).toBeDisabled();
  });

  it('the buttons open the hidden inputs', async () => {
    renderApp(<UploadDrop onPick={() => undefined} busy={false} />);
    const click = vi.spyOn(HTMLInputElement.prototype, 'click').mockImplementation(() => undefined);
    await userEvent.click(screen.getByRole('button', { name: 'Upload files' }));
    await userEvent.click(screen.getByRole('button', { name: 'Upload folder' }));
    expect(click).toHaveBeenCalledTimes(2);
  });
});
