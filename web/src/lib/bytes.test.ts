import { describe, expect, it } from 'vitest';
import { base64ToBytes, bytesToBase64, chunk, decodeUtf8, encodeUtf8, humanSize } from './bytes';

describe('bytes', () => {
  it('round-trips base64, including large inputs', () => {
    expect(bytesToBase64(encodeUtf8('hello'))).toBe('aGVsbG8=');
    expect(Array.from(base64ToBytes('aGVsbG8='))).toEqual(Array.from(encodeUtf8('hello')));
    const big = new Uint8Array(500_000).map((_, i) => i % 251);
    expect(Array.from(base64ToBytes(bytesToBase64(big)).subarray(0, 300_000)).length).toBe(300_000);
    expect(base64ToBytes(bytesToBase64(big))).toEqual(big);
  });

  it('decodes valid UTF-8 and refuses everything else', () => {
    expect(decodeUtf8(encodeUtf8('héllo ✓'))).toBe('héllo ✓');
    expect(decodeUtf8(new Uint8Array([0xff, 0xfe, 0x00]))).toBeNull();
    expect(decodeUtf8(new Uint8Array())).toBe('');
  });

  it('splits into chunks', () => {
    expect(chunk(new Uint8Array([1, 2, 3, 4, 5]), 2).map((c) => Array.from(c))).toEqual([
      [1, 2],
      [3, 4],
      [5],
    ]);
    expect(chunk(new Uint8Array(), 2)).toEqual([]);
  });

  it.each([
    [0, '0 B'],
    [1023, '1023 B'],
    [1024, '1 KiB'],
    [1536, '1.5 KiB'],
    [5 * 1024 * 1024, '5 MiB'],
    [3 * 1024 ** 4, '3 TiB'],
    [5000 * 1024 ** 4, '5000 TiB'],
  ])('humanSize(%s) = %s', (n, text) => expect(humanSize(n)).toBe(text));
});
