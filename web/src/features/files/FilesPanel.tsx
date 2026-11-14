import { useCallback, useEffect, useState } from 'react';
import { useAccess } from '../../api/access';
import { useFeatures } from '../../api/me';
import { Banner } from '../../components/Banner';
import { ErrorText } from '../../components/ErrorText';
import { browserSaveFile, type SaveFile } from '../../lib/saveFile';
import { ConfirmDialog } from '../actions/ConfirmDialog';
import { quote } from '../editor/kubectlCommand';
import { Breadcrumbs } from './Breadcrumbs';
import { FileEditor } from './FileEditor';
import { FileList } from './FileList';
import { TarError } from './tarWriter';
import { UploadDrop } from './UploadDrop';
import { planUploads, type PickedFile } from './uploadPlan';
import { FileError, useFiles, type FileContent, type FileEntry, type FileTarget } from './useFiles';

function joinPath(dir: string, name: string): string {
  return dir === '/' ? `/${name}` : `${dir}/${name}`;
}

function message(e: unknown): string {
  if (e instanceof FileError || e instanceof TarError) return e.message;
  return 'Something went wrong.';
}

interface Open {
  path: string;
  content: FileContent;
  saving: boolean;
  error?: string;
}

/** The directory listing, the file editor, upload and download of one container. */
function FilesBrowser({ target, saveFile }: { target: FileTarget; saveFile: SaveFile }) {
  const files = useFiles(target);
  const [path, setPath] = useState('/');
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [skipped, setSkipped] = useState(0);
  const [error, setError] = useState<string>();
  const [open, setOpen] = useState<Open>();
  const [confirm, setConfirm] = useState<FileEntry>();
  const [progress, setProgress] = useState<string>();
  const [busy, setBusy] = useState(false);
  const ready = files.status === 'ready';
  const { list } = files;

  const load = useCallback(
    async (p: string) => {
      setError(undefined);
      try {
        const r = await list(p);
        setPath(p);
        setEntries(r.entries);
        setSkipped(r.skipped);
        setOpen(undefined);
      } catch (e) {
        setError(message(e));
        throw e;
      }
    },
    [list],
  );

  useEffect(() => {
    if (ready) void load('/').catch(() => undefined);
  }, [ready, load, target.pod, target.container]);

  const openFile = async (p: string) => {
    setError(undefined);
    try {
      setOpen({ path: p, content: await files.read(p), saving: false });
    } catch (e) {
      setError(message(e));
    }
  };

  const openEntry = async (e: FileEntry) => {
    const full = joinPath(path, e.name);
    if (e.type === 'dir') return void load(full).catch(() => undefined);
    if (e.type === 'link') {
      const to = e.target?.startsWith('/') ? e.target : joinPath(path, e.target ?? '');
      try {
        await load(to);
        return;
      } catch {
        // not a directory: show it as a file
      }
    }
    await openFile(full);
  };

  const download = async (p: string, name: string) => {
    setError(undefined);
    try {
      const r = await files.download(p, (n) => setProgress(`Downloading ${name}: ${n} bytes`));
      saveFile(r.tar ? `${name}.tar` : name, r.data);
    } catch (e) {
      setError(message(e));
    } finally {
      setProgress(undefined);
    }
  };

  const save = async (data: Uint8Array) => {
    if (!open) return;
    setOpen({ ...open, saving: true, error: undefined });
    try {
      const stat = await files.save(open.path, data, open.content.stat);
      setOpen({ ...open, saving: false, content: { ...open.content, data, stat } });
    } catch (e) {
      setOpen({ ...open, saving: false, error: message(e) });
    }
  };

  const remove = async (e: FileEntry) => {
    setBusy(true);
    try {
      await files.remove(joinPath(path, e.name), e.type === 'dir');
      setConfirm(undefined);
      await load(path);
    } catch (err) {
      setError(message(err));
      setConfirm(undefined);
    } finally {
      setBusy(false);
    }
  };

  const upload = async (picked: PickedFile[]) => {
    if (picked.length === 0) return;
    setError(undefined);
    setBusy(true);
    try {
      const jobs = planUploads(picked);
      for (const job of jobs) {
        const name = job.kind === 'file' ? job.name : `${job.label}/`;
        await files.upload(path, job.kind === 'file' ? job.name : undefined, job.data, (sent) =>
          setProgress(`Uploading ${name}: ${Math.round((sent / Math.max(job.data.length, 1)) * 100)}%`),
        );
      }
      await load(path);
    } catch (e) {
      setError(message(e));
    } finally {
      setProgress(undefined);
      setBusy(false);
    }
  };

  return (
    <div>
      {files.status === 'lost' && (
        <Banner kind="error" action={{ label: 'Reconnect', onClick: files.reconnect }}>
          The server connection was lost.
        </Banner>
      )}
      <Breadcrumbs path={path} onNavigate={(p) => void load(p).catch(() => undefined)} />
      <ErrorText message={error} />
      {skipped > 0 && <p>{`${skipped} lines of the listing could not be read.`}</p>}
      {progress && <p role="status">{progress}</p>}
      <UploadDrop onPick={(p) => void upload(p)} busy={busy || !ready} />
      {open ? (
        <FileEditor
          path={open.path}
          content={open.content}
          saving={open.saving}
          error={open.error}
          canWrite
          onSave={(d) => void save(d)}
          onDownload={() => void download(open.path, open.path.slice(open.path.lastIndexOf('/') + 1))}
          onClose={() => setOpen(undefined)}
        />
      ) : (
        <FileList
          entries={entries}
          onOpen={(e) => void openEntry(e)}
          onDownload={(e) => void download(joinPath(path, e.name), e.name)}
          onDelete={setConfirm}
        />
      )}
      {confirm && (
        <ConfirmDialog
          title={`Delete ${joinPath(path, confirm.name)}?`}
          confirmLabel="Delete"
          destructive
          busy={busy}
          kubectl={`kubectl exec -n ${quote(target.namespace)} ${quote(target.pod)} -c ${quote(target.container)} -- rm ${confirm.type === 'dir' ? '-rf' : '-f'} -- ${quote(joinPath(path, confirm.name))}`}
          onConfirm={() => void remove(confirm)}
          onCancel={() => setConfirm(undefined)}
        >
          <p>
            {confirm.type === 'dir'
              ? 'The directory and everything in it will be removed.'
              : 'The file will be removed.'}
          </p>
        </ConfirmDialog>
      )}
    </div>
  );
}

/** The file browser of a container; hidden unless the feature is on and the user may exec in the pod. */
export function FilesPanel({
  target,
  saveFile = browserSaveFile,
}: {
  target: FileTarget;
  saveFile?: SaveFile;
}) {
  const features = useFeatures();
  const access = useAccess(
    target.cluster,
    [
      {
        verb: 'create',
        group: '',
        resource: 'pods',
        subresource: 'exec',
        namespace: target.namespace,
        name: target.pod,
      },
    ],
    features?.fileBrowser === true,
  );
  if (features?.fileBrowser !== true || access.data?.[0]?.allowed !== true) return null;
  return <FilesBrowser target={target} saveFile={saveFile} />;
}
