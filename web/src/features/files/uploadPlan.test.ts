import { describe, expect, it } from 'vitest';
import { decodeUtf8, encodeUtf8 } from '../../lib/bytes';
import { planUploads, readEntries, type FsEntry } from './uploadPlan';
import { TarError } from './tarWriter';

const data = (s: string) => encodeUtf8(s);

describe('planUploads', () => {
  it('uploads plain files one by one', () => {
    expect(
      planUploads([
        { path: 'a.txt', data: data('a') },
        { path: 'b.txt', data: data('b') },
      ]),
    ).toEqual([
      { kind: 'file', name: 'a.txt', data: data('a') },
      { kind: 'file', name: 'b.txt', data: data('b') },
    ]);
  });

  it('packs a directory with its subdirectories into one tar', () => {
    const jobs = planUploads([
      { path: 'site/index.html', data: data('<p>') },
      { path: 'site/css/app/main.css', data: data('x') },
      { path: 'single.txt', data: data('s') },
    ]);
    expect(jobs.map((j) => j.kind)).toEqual(['file', 'tar']);
    const tar = jobs[1];
    expect(tar).toMatchObject({ kind: 'tar', label: 'site' });
    const names: string[] = [];
    const bytes = (tar as { data: Uint8Array }).data;
    for (let at = 0; at + 512 <= bytes.length; at += 512) {
      const header = bytes.subarray(at, at + 100);
      const name = decodeUtf8(header)?.split('\0')[0] ?? '';
      if (name === '') break;
      names.push(name);
      const size = Number.parseInt(decodeUtf8(bytes.subarray(at + 124, at + 135)) ?? '0', 8);
      at += Math.ceil(size / 512) * 512;
    }
    expect(names).toEqual([
      'site/',
      'site/css/',
      'site/css/app/',
      'site/index.html',
      'site/css/app/main.css',
    ]);
  });

  it('makes one tar per directory', () => {
    const jobs = planUploads([
      { path: 'a/x', data: data('1') },
      { path: 'b/y', data: data('2') },
    ]);
    expect(jobs.map((j) => (j.kind === 'tar' ? j.label : ''))).toEqual(['a', 'b']);
  });

  it('refuses paths that are too long for a tar header, with a clear error', () => {
    expect(() => planUploads([{ path: `dir/${'x'.repeat(120)}`, data: data('1') }])).toThrow(TarError);
  });

  it('is empty without files', () => {
    expect(planUploads([])).toEqual([]);
  });
});

function fileEntry(name: string, content: string): FsEntry {
  return { isFile: true, isDirectory: false, name, file: (ok) => ok(new File([content], name)) };
}

function dirEntry(name: string, children: FsEntry[]): FsEntry {
  let served = false;
  return {
    isFile: false,
    isDirectory: true,
    name,
    createReader: () => ({
      readEntries: (ok) => {
        ok(served ? [] : children);
        served = true;
      },
    }),
  };
}

describe('readEntries', () => {
  it('reads files and nested directories with relative paths', async () => {
    const got = await readEntries([
      fileEntry('a.txt', 'A'),
      dirEntry('d', [fileEntry('b.txt', 'B'), dirEntry('e', [fileEntry('c.txt', 'C')])]),
    ]);
    expect(got.map((g) => g.path)).toEqual(['a.txt', 'd/b.txt', 'd/e/c.txt']);
    expect(await got[2]?.file.text()).toBe('C');
  });

  it('skips entries that are neither files nor directories and directories without a reader', async () => {
    const got = await readEntries([
      { isFile: false, isDirectory: false, name: 'x' },
      { isFile: false, isDirectory: true, name: 'noreader' },
    ]);
    expect(got).toEqual([]);
  });
});
