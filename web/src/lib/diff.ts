export type DiffLine = { type: 'same' | 'add' | 'del'; text: string };

/** Above this many cells the middle part is shown as one removed and one added block instead of an LCS. */
const MAX_CELLS = 4_000_000;

function split(text: string): string[] {
  if (text === '') return [];
  const lines = text.split('\n');
  if (lines[lines.length - 1] === '') lines.pop();
  return lines;
}

/** A plain line diff from the longest common subsequence; common lines at both ends are matched first. */
export function diffLines(before: string, after: string): DiffLine[] {
  const a = split(before);
  const b = split(after);
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start++;
  let endA = a.length;
  let endB = b.length;
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA--;
    endB--;
  }
  const out: DiffLine[] = a.slice(0, start).map((text) => ({ type: 'same', text }));
  out.push(...middle(a.slice(start, endA), b.slice(start, endB)));
  out.push(...a.slice(endA).map((text): DiffLine => ({ type: 'same', text })));
  return out;
}

function middle(a: string[], b: string[]): DiffLine[] {
  if (a.length === 0 || b.length === 0 || a.length * b.length > MAX_CELLS) {
    return [
      ...a.map((text): DiffLine => ({ type: 'del', text })),
      ...b.map((text): DiffLine => ({ type: 'add', text })),
    ];
  }
  const w = b.length + 1;
  const table = new Uint32Array((a.length + 1) * w);
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      table[i * w + j] =
        a[i] === b[j]
          ? (table[(i + 1) * w + j + 1] ?? 0) + 1
          : Math.max(table[(i + 1) * w + j] ?? 0, table[i * w + j + 1] ?? 0);
    }
  }
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      out.push({ type: 'same', text: a[i] ?? '' });
      i++;
      j++;
    } else if ((table[(i + 1) * w + j] ?? 0) >= (table[i * w + j + 1] ?? 0)) {
      out.push({ type: 'del', text: a[i] ?? '' });
      i++;
    } else {
      out.push({ type: 'add', text: b[j] ?? '' });
      j++;
    }
  }
  for (; i < a.length; i++) out.push({ type: 'del', text: a[i] ?? '' });
  for (; j < b.length; j++) out.push({ type: 'add', text: b[j] ?? '' });
  return out;
}

export function hasChanges(diff: DiffLine[]): boolean {
  return diff.some((l) => l.type !== 'same');
}
