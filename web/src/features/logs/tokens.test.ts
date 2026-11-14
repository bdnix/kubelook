import { describe, expect, it } from 'vitest';
import { tokenize } from './tokens';

describe('tokenize', () => {
  it('returns one text token for plain text', () => {
    expect(tokenize('hello world')).toEqual([{ type: 'text', text: 'hello world' }]);
    expect(tokenize('')).toEqual([]);
  });

  it('finds timestamps, UUIDs, URLs and bracketed logger names', () => {
    const text =
      '2026-11-14T12:00:00.123Z [controller.manager] GET https://example.com/a?b=1 id=123e4567-e89b-12d3-a456-426614174000 ok';
    expect(tokenize(text)).toEqual([
      { type: 'timestamp', text: '2026-11-14T12:00:00.123Z' },
      { type: 'text', text: ' ' },
      { type: 'logger', text: '[controller.manager]' },
      { type: 'text', text: ' GET ' },
      { type: 'url', text: 'https://example.com/a?b=1' },
      { type: 'text', text: ' id=' },
      { type: 'uuid', text: '123e4567-e89b-12d3-a456-426614174000' },
      { type: 'text', text: ' ok' },
    ]);
  });

  it('the tokens always add up to the original text', () => {
    const text = 'x 2026-11-14 12:00:00 [a] http://h/p?q=1&r=2 (https://b.example/x). tail';
    expect(
      tokenize(text)
        .map((t) => t.text)
        .join(''),
    ).toBe(text);
  });

  it('recognises a date and time with a space and with an offset', () => {
    expect(tokenize('2026-11-14 12:00:00,123 x')[0]).toEqual({
      type: 'timestamp',
      text: '2026-11-14 12:00:00,123',
    });
    expect(tokenize('2026-11-14T12:00:00+01:00 x')[0]).toEqual({
      type: 'timestamp',
      text: '2026-11-14T12:00:00+01:00',
    });
  });

  it('a URL stops before closing punctuation', () => {
    const urls = tokenize('see (https://example.com/x), or "http://a.b/c".').filter((t) => t.type === 'url');
    expect(urls.map((u) => u.text)).toEqual(['https://example.com/x', 'http://a.b/c']);
  });

  it('only http and https links are URLs', () => {
    expect(tokenize('javascript:alert(1) ftp://x/y file:///etc/passwd').every((t) => t.type === 'text')).toBe(
      true,
    );
  });

  it('is not fooled by things that only look like logger names', () => {
    expect(tokenize('array[0] and []')).toEqual([{ type: 'text', text: 'array[0] and []' }]);
  });

  it('copes with very long lines without backtracking blow-ups', () => {
    const long = 'a'.repeat(100_000) + ' ' + '['.repeat(1000);
    expect(
      tokenize(long)
        .map((t) => t.text)
        .join(''),
    ).toBe(long);
  });
});
