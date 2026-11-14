import { buildTar, type TarEntry } from './tarWriter';

export interface PickedFile {
  /** Path relative to the place the user picked or dropped, with `/` separators. */
  path: string;
  data: Uint8Array;
}

export type UploadJob =
  { kind: 'file'; name: string; data: Uint8Array } | { kind: 'tar'; label: string; data: Uint8Array };

/**
 * Plain files upload one by one; each dropped directory becomes one tar archive that holds the directory, its
 * subdirectories and files. Throws TarError for paths a tar header cannot hold.
 */
export function planUploads(files: PickedFile[]): UploadJob[] {
  const jobs: UploadJob[] = [];
  const dirs = new Map<string, PickedFile[]>();
  for (const f of files) {
    const slash = f.path.indexOf('/');
    if (slash < 0) {
      jobs.push({ kind: 'file', name: f.path, data: f.data });
      continue;
    }
    const top = f.path.slice(0, slash);
    dirs.set(top, [...(dirs.get(top) ?? []), f]);
  }
  for (const [top, members] of dirs) {
    const dirPaths = new Set<string>([top]);
    for (const m of members) {
      const parts = m.path.split('/');
      for (let i = 2; i < parts.length; i++) dirPaths.add(parts.slice(0, i).join('/'));
    }
    const entries: TarEntry[] = [
      ...[...dirPaths].sort().map((path): TarEntry => ({ path, dir: true })),
      ...members.map((m): TarEntry => ({ path: m.path, data: m.data })),
    ];
    jobs.push({ kind: 'tar', label: top, data: buildTar(entries) });
  }
  return jobs;
}

/** The parts of the browser's FileSystemEntry API that reading a dropped directory needs. */
export interface FsEntry {
  isFile: boolean;
  isDirectory: boolean;
  name: string;
  file?: (ok: (f: File) => void, fail: (e: unknown) => void) => void;
  createReader?: () => { readEntries(ok: (e: FsEntry[]) => void, fail: (e: unknown) => void): void };
}

async function allEntries(dir: FsEntry): Promise<FsEntry[]> {
  const reader = dir.createReader?.();
  const out: FsEntry[] = [];
  if (!reader) return out;
  for (;;) {
    const batch = await new Promise<FsEntry[]>((ok, fail) => reader.readEntries(ok, fail));
    if (batch.length === 0) return out;
    out.push(...batch);
  }
}

/** Reads dropped entries (files and directories, recursively) into picked files with relative paths. */
export async function readEntries(entries: FsEntry[], prefix = ''): Promise<{ path: string; file: File }[]> {
  const out: { path: string; file: File }[] = [];
  for (const e of entries) {
    if (e.isFile && e.file) {
      const file = await new Promise<File>((ok, fail) => e.file?.(ok, fail));
      out.push({ path: `${prefix}${e.name}`, file });
    } else if (e.isDirectory) {
      out.push(...(await readEntries(await allEntries(e), `${prefix}${e.name}/`)));
    }
  }
  return out;
}
