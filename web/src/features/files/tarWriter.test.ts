import { describe, expect, it } from 'vitest';
import { decodeUtf8, encodeUtf8 } from '../../lib/bytes';
import { buildTar, tarHeader, TarError } from './tarWriter';

const text = (b: Uint8Array) => decodeUtf8(b) ?? '';
const cstr = (b: Uint8Array) => text(b).split('\0')[0] ?? '';

describe('tarHeader', () => {
  it('is 512 bytes with the name, ustar magic, size, type and a valid checksum', () => {
    const h = tarHeader({ path: 'dir/a.txt', data: encodeUtf8('hello'), mtime: 1_700_000_000 });
    expect(h).toHaveLength(512);
    expect(cstr(h.subarray(0, 100))).toBe('dir/a.txt');
    expect(cstr(h.subarray(124, 136))).toBe('00000000005');
    expect(cstr(h.subarray(136, 148))).toBe((1_700_000_000).toString(8).padStart(11, '0'));
    expect(String.fromCharCode(h[156] as number)).toBe('0');
    expect(text(h.subarray(257, 263))).toBe('ustar\0');
    expect(text(h.subarray(263, 265))).toBe('00');
    // the checksum: the sum of all bytes with the checksum field read as spaces
    const stored = Number.parseInt(text(h.subarray(148, 154)), 8);
    const copy = h.slice();
    copy.fill(0x20, 148, 156);
    expect(stored).toBe(copy.reduce((a, b) => a + b, 0));
    expect(text(h.subarray(154, 156))).toBe('\0 ');
  });

  it('marks directories with a slash, type 5 and size 0', () => {
    const h = tarHeader({ path: 'dir', dir: true, data: encodeUtf8('ignored') });
    expect(cstr(h.subarray(0, 100))).toBe('dir/');
    expect(String.fromCharCode(h[156] as number)).toBe('5');
    expect(cstr(h.subarray(124, 136))).toBe('00000000000');
    expect(cstr(h.subarray(100, 108))).toBe('0000755');
  });

  it('refuses names longer than 100 bytes, counting bytes and not characters', () => {
    expect(() => tarHeader({ path: 'a'.repeat(101) })).toThrow(TarError);
    expect(() => tarHeader({ path: 'a'.repeat(100) })).not.toThrow();
    expect(() => tarHeader({ path: 'é'.repeat(51) })).toThrow(/longer than 100 bytes/);
  });

  it('refuses a size that does not fit in the header', () => {
    const big = { path: 'x', data: { length: 8 ** 11 } as unknown as Uint8Array };
    expect(() => tarHeader(big)).toThrow(TarError);
  });
});

describe('buildTar', () => {
  it('pads data to whole blocks and ends with two empty blocks', () => {
    const tar = buildTar([
      { path: 'd', dir: true },
      { path: 'd/a', data: encodeUtf8('x'.repeat(513)) },
      { path: 'd/empty', data: new Uint8Array() },
    ]);
    // header(d) + header(a) + 2 blocks of data + header(empty) + 2 end blocks
    expect(tar).toHaveLength(512 * (1 + 1 + 2 + 1 + 2));
    expect(cstr(tar.subarray(0, 100))).toBe('d/');
    expect(cstr(tar.subarray(512, 612))).toBe('d/a');
    expect(text(tar.subarray(1024, 1024 + 513))).toBe('x'.repeat(513));
    expect(tar.subarray(tar.length - 1024).every((b) => b === 0)).toBe(true);
  });

  it('an empty list is only the end blocks', () => {
    expect(buildTar([])).toHaveLength(1024);
  });
});
