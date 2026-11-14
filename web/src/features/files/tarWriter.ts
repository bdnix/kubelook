import { encodeUtf8 } from '../../lib/bytes';

const BLOCK = 512;
const NAME_MAX = 100;

export interface TarEntry {
  /** A relative path with `/` separators; directories end without a slash and set `dir`. */
  path: string;
  data?: Uint8Array;
  dir?: boolean;
  /** Seconds since the epoch; 0 when unknown. */
  mtime?: number;
}

export class TarError extends Error {}

function octal(value: number, width: number): Uint8Array {
  // `width` bytes: octal digits, zero padded, ending with a NUL.
  const text = value.toString(8).padStart(width - 1, '0');
  if (text.length > width - 1) throw new TarError('A value does not fit in a tar header.');
  return encodeUtf8(`${text}\0`);
}

function field(target: Uint8Array, offset: number, bytes: Uint8Array): void {
  target.set(bytes, offset);
}

/** The 512-byte ustar header of one entry. Names longer than 100 bytes are refused. */
export function tarHeader(entry: TarEntry): Uint8Array {
  const name = entry.dir ? `${entry.path}/` : entry.path;
  const nameBytes = encodeUtf8(name);
  if (nameBytes.length > NAME_MAX) {
    throw new TarError(`The path "${entry.path}" is longer than 100 bytes, which a tar header cannot hold.`);
  }
  const size = entry.dir ? 0 : (entry.data?.length ?? 0);
  const h = new Uint8Array(BLOCK);
  field(h, 0, nameBytes);
  field(h, 100, octal(entry.dir ? 0o755 : 0o644, 8));
  field(h, 108, octal(0, 8));
  field(h, 116, octal(0, 8));
  field(h, 124, octal(size, 12));
  field(h, 136, octal(entry.mtime ?? 0, 12));
  h.fill(0x20, 148, 156); // the checksum is computed with this field as spaces
  h[156] = (entry.dir ? '5' : '0').charCodeAt(0);
  field(h, 257, encodeUtf8('ustar\0'));
  field(h, 263, encodeUtf8('00'));
  const sum = h.reduce((a, b) => a + b, 0);
  field(h, 148, encodeUtf8(`${sum.toString(8).padStart(6, '0')}\0 `));
  return h;
}

/** A tar archive of the entries: a header and zero-padded data for each, then two empty blocks. */
export function buildTar(entries: TarEntry[]): Uint8Array {
  const parts: Uint8Array[] = [];
  for (const e of entries) {
    parts.push(tarHeader(e));
    if (!e.dir && e.data && e.data.length > 0) {
      parts.push(e.data);
      const pad = (BLOCK - (e.data.length % BLOCK)) % BLOCK;
      if (pad > 0) parts.push(new Uint8Array(pad));
    }
  }
  parts.push(new Uint8Array(BLOCK * 2));
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let at = 0;
  for (const p of parts) {
    out.set(p, at);
    at += p.length;
  }
  return out;
}
