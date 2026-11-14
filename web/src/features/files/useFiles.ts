import { useCallback, useEffect, useRef, useState } from 'react';
import { base64ToBytes, bytesToBase64, chunk } from '../../lib/bytes';
import { useSocket } from '../../ws/context';
import type { Channel } from '../../ws/socket';

export interface FileTarget {
  cluster: string;
  namespace: string;
  pod: string;
  container: string;
}

export interface FileEntry {
  name: string;
  type: 'file' | 'dir' | 'link' | 'other';
  mode: string;
  size: number;
  modified: string;
  target?: string;
}

export interface FileStat {
  size: number;
  modified: string;
}

export interface FileContent {
  data: Uint8Array;
  binary: boolean;
  truncated: boolean;
  stat: FileStat;
}

export class FileError extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.name = 'FileError';
    this.code = code;
  }
}

export const CHUNK_BYTES = 48 * 1024;

export type FilesStatus = 'connecting' | 'ready' | 'lost';

interface Pending {
  resolve(frame: Record<string, unknown>): void;
  reject(e: Error): void;
  chunks: Uint8Array[];
  onChunk?: (received: number) => void;
}

export interface Files {
  status: FilesStatus;
  list(path: string): Promise<{ entries: FileEntry[]; skipped: number }>;
  read(path: string): Promise<FileContent>;
  save(path: string, data: Uint8Array, expect: FileStat | undefined): Promise<FileStat>;
  remove(path: string, recursive: boolean): Promise<void>;
  download(path: string, onProgress?: (bytes: number) => void): Promise<{ data: Uint8Array; tar: boolean }>;
  /** `name` is the file name; without it `data` is a tar archive that is extracted in `dir`. */
  upload(
    dir: string,
    name: string | undefined,
    data: Uint8Array,
    onProgress?: (sent: number) => void,
  ): Promise<void>;
  reconnect(): void;
}

function join(chunks: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(chunks.reduce((n, c) => n + c.length, 0));
  let at = 0;
  for (const c of chunks) {
    out.set(c, at);
    at += c.length;
  }
  return out;
}

/** One file browser channel on a container; every call is a request with an id that the reply echoes. */
export function useFiles(target: FileTarget | undefined): Files {
  const socket = useSocket();
  const [status, setStatus] = useState<FilesStatus>('connecting');
  const [generation, setGeneration] = useState(0);
  const channel = useRef<Channel | null>(null);
  const pending = useRef(new Map<string, Pending>());
  const uploading = useRef<Pending | null>(null);
  const counter = useRef(0);
  const { cluster, namespace, pod, container } = target ?? {
    cluster: '',
    namespace: '',
    pod: '',
    container: '',
  };
  const enabled = target !== undefined;

  useEffect(() => {
    if (!enabled) return undefined;
    setStatus('connecting');
    const requests = pending.current;
    const failAll = (e: Error) => {
      for (const p of requests.values()) p.reject(e);
      requests.clear();
      uploading.current?.reject(e);
      uploading.current = null;
    };
    const ch = socket.open(
      'file.open',
      { cluster, namespace, pod, container },
      {
        onFrame(type, data) {
          const d = (data ?? {}) as Record<string, unknown> & { rid?: string };
          switch (type) {
            case 'opened':
              setStatus('ready');
              return;
            case 'error': {
              const err = new FileError(
                String(d.code ?? 'internal'),
                String(d.message ?? 'The file operation failed.'),
              );
              const p = typeof d.rid === 'string' ? requests.get(d.rid) : uploading.current;
              if (p) {
                if (typeof d.rid === 'string') requests.delete(d.rid);
                else uploading.current = null;
                p.reject(err);
              } else if (d.rid === undefined) {
                failAll(err);
              }
              return;
            }
            case 'closed':
              failAll(new FileError('closed', 'The file browser connection was closed.'));
              return;
          }
          const rid = typeof d.rid === 'string' ? d.rid : '';
          const p = requests.get(rid);
          if (!p) return;
          if (type === 'chunk') {
            p.chunks.push(base64ToBytes(String(d.data ?? '')));
            p.onChunk?.(p.chunks.reduce((n, c) => n + c.length, 0));
            return;
          }
          requests.delete(rid);
          p.resolve(d);
        },
        onConnectionLost() {
          setStatus('lost');
          failAll(new FileError('lost', 'The server connection was lost.'));
        },
      },
    );
    channel.current = ch;
    return () => {
      ch.close();
      channel.current = null;
      failAll(new FileError('closed', 'The file browser was closed.'));
    };
  }, [socket, enabled, cluster, namespace, pod, container, generation]);

  const request = useCallback((op: string, body: Record<string, unknown>, onChunk?: (n: number) => void) => {
    return new Promise<Pending & { frame: Record<string, unknown> }>((resolve, reject) => {
      const ch = channel.current;
      if (!ch) {
        reject(new FileError('closed', 'The file browser is not connected.'));
        return;
      }
      const rid = `r${++counter.current}`;
      const entry: Pending = {
        chunks: [],
        onChunk,
        resolve: (frame) => resolve({ ...entry, frame }),
        reject,
      };
      pending.current.set(rid, entry);
      ch.send(op, { rid, ...body });
    });
  }, []);

  const list = useCallback(
    async (path: string) => {
      const { frame } = await request('file.list', { path });
      return { entries: frame.entries as FileEntry[], skipped: Number(frame.skipped ?? 0) };
    },
    [request],
  );
  const read = useCallback(
    async (path: string): Promise<FileContent> => {
      const { frame } = await request('file.read', { path });
      return {
        data: base64ToBytes(String(frame.data ?? '')),
        binary: frame.binary === true,
        truncated: frame.truncated === true,
        stat: frame.stat as FileStat,
      };
    },
    [request],
  );
  const save = useCallback(
    async (path: string, data: Uint8Array, expect: FileStat | undefined) => {
      const { frame } = await request('file.save', {
        path,
        data: bytesToBase64(data),
        ...(expect ? { expect } : {}),
      });
      return frame.stat as FileStat;
    },
    [request],
  );
  const remove = useCallback(
    async (path: string, recursive: boolean) => {
      await request('file.delete', { path, recursive });
    },
    [request],
  );
  const download = useCallback(
    async (path: string, onProgress?: (bytes: number) => void) => {
      const { frame, chunks } = await request('file.download', { path }, onProgress);
      return { data: join(chunks), tar: frame.tar === true };
    },
    [request],
  );
  const upload = useCallback(
    (dir: string, name: string | undefined, data: Uint8Array, onProgress?: (sent: number) => void) =>
      new Promise<void>((resolve, reject) => {
        const ch = channel.current;
        if (!ch) {
          reject(new FileError('closed', 'The file browser is not connected.'));
          return;
        }
        const rid = `r${++counter.current}`;
        const entry: Pending = {
          chunks: [],
          resolve: () => {
            if (uploading.current === entry) uploading.current = null;
            resolve();
          },
          reject,
        };
        pending.current.set(rid, entry);
        uploading.current = entry;
        ch.send('file.upload.begin', { dir, ...(name === undefined ? { tar: true } : { name }) });
        let sent = 0;
        chunk(data, CHUNK_BYTES).forEach((piece, seq) => {
          ch.send('file.upload.chunk', { seq, data: bytesToBase64(piece) });
          sent += piece.length;
          onProgress?.(sent);
        });
        ch.send('file.upload.end', { rid });
      }),
    [],
  );

  return { status, list, read, save, remove, download, upload, reconnect: () => setGeneration((g) => g + 1) };
}
