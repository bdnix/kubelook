import type { LogLine } from './logBuffer';
import { LEVELS, parseLine, type Level, type ParsedLine } from './parse';

export { LEVELS, type Level };

/** The level as text: colour is never the only signal. */
export const levelLabel = (level: Level | undefined): string => level ?? '';

export const levelClass = (level: Level | undefined): string =>
  level ? `level-${level.toLowerCase()}` : 'level-none';

export interface LineFilter {
  /** Show only these levels; absent shows all. */
  levels?: ReadonlySet<string>;
  /** Show only JSON lines whose field has this value (compared as text). */
  field?: { key: string; value: string };
}

const parsedCache = new WeakMap<LogLine, ParsedLine>();

/** The parsed form of a line, computed once per line. */
export function parsed(line: LogLine): ParsedLine {
  let p = parsedCache.get(line);
  if (!p) {
    p = parseLine(line.text);
    parsedCache.set(line, p);
  }
  return p;
}

/**
 * Applies the level and field filters. Markers always stay. Without a field filter, plain lines and JSON lines
 * without a level stay too; a field filter hides every line that is not a JSON line with that value.
 */
export function filterLines(lines: LogLine[], f: LineFilter): LogLine[] {
  const field = f.field && f.field.key !== '' ? f.field : undefined;
  if (!f.levels && !field) return lines;
  return lines.filter((line) => {
    if (line.marker) return true;
    const p = parsed(line);
    if (field && !(p.json && String(p.fields[field.key] ?? '') === field.value && field.key in p.fields))
      return false;
    if (f.levels && p.json && p.level && !f.levels.has(p.level)) return false;
    return true;
  });
}

const SOURCE_COLORS = 12;

/** A stable colour number (0–11) per pod and container. */
export function sourceColor(pod: string, container: string): number {
  let h = 0x811c9dc5; // FNV-1a
  for (const ch of `${pod}/${container}`) h = Math.imul(h ^ (ch.codePointAt(0) ?? 0), 0x01000193);
  return (h >>> 0) % SOURCE_COLORS;
}
