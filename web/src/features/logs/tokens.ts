export type TokenType = 'timestamp' | 'uuid' | 'url' | 'logger' | 'text';

export interface Token {
  type: TokenType;
  text: string;
}

// One pass over the text; every alternative is anchored on a literal start, so it runs in linear time.
const TOKENS = new RegExp(
  [
    String.raw`(?<timestamp>\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d{1,9})?(?:Z|[+-]\d{2}:?\d{2})?)`,
    String.raw`(?<uuid>\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b)`,
    String.raw`(?<url>https?://[^\s<>"'\`)\]]+)`,
    String.raw`(?<logger>\[[A-Za-z][\w./-]{0,63}\])`,
  ].join('|'),
  'g',
);

const TRAILING = new Set(['.', ',', ';', ':', '!', '?']);

/** Splits log text into timestamps, UUIDs, http(s) URLs, bracketed logger names and the text in between. */
export function tokenize(text: string): Token[] {
  const out: Token[] = [];
  const push = (type: TokenType, t: string) => {
    if (t === '') return;
    const last = out[out.length - 1];
    if (type === 'text' && last?.type === 'text') last.text += t;
    else out.push({ type, text: t });
  };
  let at = 0;
  for (const m of text.matchAll(TOKENS)) {
    const groups = m.groups ?? {};
    const type = (Object.keys(groups).find((k) => groups[k] !== undefined) ?? 'text') as TokenType;
    let found = m[0];
    if (type === 'url') {
      let end = found.length;
      while (end > 0 && TRAILING.has(found.charAt(end - 1))) end--;
      found = found.slice(0, end);
    }
    push('text', text.slice(at, m.index));
    push(type, found);
    at = m.index + found.length;
  }
  push('text', text.slice(at));
  return out;
}
