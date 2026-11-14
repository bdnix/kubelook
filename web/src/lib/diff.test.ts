import { describe, expect, it } from 'vitest';
import { diffLines, hasChanges } from './diff';

const render = (a: string, b: string) =>
  diffLines(a, b).map((l) => `${l.type === 'same' ? ' ' : l.type === 'add' ? '+' : '-'}${l.text}`);

describe('diffLines', () => {
  it('marks identical text as unchanged', () => {
    expect(render('a\nb\n', 'a\nb\n')).toEqual([' a', ' b']);
    expect(hasChanges(diffLines('a\nb\n', 'a\nb\n'))).toBe(false);
  });

  it('shows added, removed and changed lines', () => {
    expect(render('a\nb\nc\n', 'a\nc\n')).toEqual([' a', '-b', ' c']);
    expect(render('a\nc\n', 'a\nb\nc\n')).toEqual([' a', '+b', ' c']);
    expect(render('a\nb\nc\n', 'a\nB\nc\n')).toEqual([' a', '-b', '+B', ' c']);
    expect(hasChanges(diffLines('a', 'b'))).toBe(true);
  });

  it('finds the longest common subsequence in the middle', () => {
    expect(render('x\na\nb\nc\ny\n', 'x\nb\nd\nc\ny\n')).toEqual([' x', '-a', ' b', '+d', ' c', ' y']);
    expect(render('1\n2\n3\n4\n', '2\n4\n5\n')).toEqual(['-1', ' 2', '-3', ' 4', '+5']);
  });

  it('handles empty input on either side', () => {
    expect(render('', 'a\n')).toEqual(['+a']);
    expect(render('a\n', '')).toEqual(['-a']);
    expect(render('', '')).toEqual([]);
  });

  it('ignores a missing trailing newline', () => {
    expect(render('a\nb', 'a\nb\n')).toEqual([' a', ' b']);
  });

  it('shows a huge middle part as one block instead of building the table', () => {
    const a = Array.from({ length: 3000 }, (_, i) => `a${i}`).join('\n');
    const b = Array.from({ length: 3000 }, (_, i) => `b${i}`).join('\n');
    const d = diffLines(a, b);
    expect(d).toHaveLength(6000);
    expect(d.slice(0, 3000).every((l) => l.type === 'del')).toBe(true);
    expect(d.slice(3000).every((l) => l.type === 'add')).toBe(true);
  });
});
