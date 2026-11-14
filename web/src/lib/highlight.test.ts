import { describe, expect, it } from 'vitest';
import { MAX_MATCHES, highlight, matches } from './highlight';

describe('highlight', () => {
  it('returns the whole text for an empty query', () => {
    expect(highlight('hello', '', false)).toEqual({ segments: [{ text: 'hello', match: false }] });
  });

  it('marks every case-insensitive match of a plain query', () => {
    const { segments } = highlight('Error: error ERROR', 'error', false);
    expect(segments.filter((s) => s.match).map((s) => s.text)).toEqual(['Error', 'error', 'ERROR']);
    expect(segments.map((s) => s.text).join('')).toBe('Error: error ERROR');
  });

  it('treats regex characters in a plain query literally', () => {
    expect(highlight('a.b a+b', 'a.b', false).segments.filter((s) => s.match)).toHaveLength(1);
    expect(highlight('a(b', '(', false).segments.filter((s) => s.match)).toHaveLength(1);
  });

  it('supports regular expressions', () => {
    const { segments } = highlight('id=42 id=7', 'id=\\d+', true);
    expect(segments.filter((s) => s.match).map((s) => s.text)).toEqual(['id=42', 'id=7']);
  });

  it('reports an invalid regular expression and highlights nothing', () => {
    const h = highlight('text', '(', true);
    expect(h.error).toBeTruthy();
    expect(h.segments).toEqual([{ text: 'text', match: false }]);
  });

  it('ignores empty matches and returns the text when nothing matches', () => {
    expect(highlight('abc', 'x*', true).segments).toEqual([{ text: 'abc', match: false }]);
    expect(highlight('abc', 'zzz', false).segments).toEqual([{ text: 'abc', match: false }]);
  });

  it('highlights at most 100 matches per line', () => {
    const { segments } = highlight('a'.repeat(500), 'a', false);
    expect(segments.filter((s) => s.match)).toHaveLength(MAX_MATCHES);
    expect(segments.map((s) => s.text).join('')).toBe('a'.repeat(500));
  });
});

describe('matches', () => {
  it('filters by plain text or regex; an invalid regex filters nothing', () => {
    expect(matches('Hello', 'hello', false)).toBe(true);
    expect(matches('Hello', 'bye', false)).toBe(false);
    expect(matches('id=42', 'id=\\d+', true)).toBe(true);
    expect(matches('x', '(', true)).toBe(true);
    expect(matches('x', '', false)).toBe(true);
  });
});
