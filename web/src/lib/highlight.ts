export interface Segment {
  text: string;
  match: boolean;
}

export interface Highlight {
  segments: Segment[];
  /** Set when the query is a regular expression that does not compile. */
  error?: string;
}

/** Matches highlighted per line at most, so a pathological pattern cannot freeze the tab. */
export const MAX_MATCHES = 100;

function build(query: string, regex: boolean): RegExp {
  const source = regex ? query : query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return new RegExp(source, 'gi');
}

/** Splits `text` into plain and matching segments for `query` (plain text or a regular expression). */
export function highlight(text: string, query: string, regex: boolean): Highlight {
  if (query === '') return { segments: [{ text, match: false }] };
  let re: RegExp;
  try {
    re = build(query, regex);
  } catch (err) {
    return {
      segments: [{ text, match: false }],
      error: err instanceof Error ? err.message : 'Invalid regular expression',
    };
  }
  const segments: Segment[] = [];
  let last = 0;
  let found = 0;
  for (const m of text.matchAll(re)) {
    if (m[0] === '') continue; // an empty match highlights nothing
    if (found >= MAX_MATCHES) break;
    found++;
    if (m.index > last) segments.push({ text: text.slice(last, m.index), match: false });
    segments.push({ text: m[0], match: true });
    last = m.index + m[0].length;
  }
  if (last < text.length) segments.push({ text: text.slice(last), match: false });
  return { segments: segments.length > 0 ? segments : [{ text, match: false }] };
}

/** Whether `text` contains a match (an invalid expression matches everything: it filters nothing). */
export function matches(text: string, query: string, regex: boolean): boolean {
  if (query === '') return true;
  try {
    return build(query, regex).test(text);
  } catch {
    return true;
  }
}
