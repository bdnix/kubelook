import { useRef, useState } from 'react';
import { Button } from '../../components/Button';
import { readEntries, type FsEntry, type PickedFile } from './uploadPlan';

async function pickFromInput(list: FileList): Promise<PickedFile[]> {
  const out: PickedFile[] = [];
  for (const f of Array.from(list)) {
    const rel = (f as File & { webkitRelativePath?: string }).webkitRelativePath;
    out.push({ path: rel && rel !== '' ? rel : f.name, data: new Uint8Array(await f.arrayBuffer()) });
  }
  return out;
}

async function pickFromDrop(dt: DataTransfer): Promise<PickedFile[]> {
  const entries = Array.from(dt.items)
    .map((i) => i.webkitGetAsEntry() as unknown as FsEntry | null)
    .filter((e): e is FsEntry => e !== null);
  const read = await readEntries(entries);
  return Promise.all(
    read.map(async (r) => ({ path: r.path, data: new Uint8Array(await r.file.arrayBuffer()) })),
  );
}

/** Upload by button (files or a folder) or by dropping files and folders. */
export function UploadDrop({ onPick, busy }: { onPick: (files: PickedFile[]) => void; busy: boolean }) {
  const files = useRef<HTMLInputElement>(null);
  const folder = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  return (
    <div
      role="region"
      aria-label="Drop files here"
      className={over ? 'drop drop-over' : 'drop'}
      onDragOver={(e) => {
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setOver(false);
        void pickFromDrop(e.dataTransfer).then(onPick);
      }}
    >
      <Button onClick={() => files.current?.click()} disabled={busy}>
        Upload files
      </Button>
      <Button onClick={() => folder.current?.click()} disabled={busy}>
        Upload folder
      </Button>
      <input
        ref={files}
        type="file"
        multiple
        hidden
        aria-label="Choose files"
        onChange={(e) => e.target.files && void pickFromInput(e.target.files).then(onPick)}
      />
      <input
        ref={folder}
        type="file"
        hidden
        aria-label="Choose a folder"
        {...{ webkitdirectory: '' }}
        onChange={(e) => e.target.files && void pickFromInput(e.target.files).then(onPick)}
      />
      <span> or drop files and folders here</span>
    </div>
  );
}
