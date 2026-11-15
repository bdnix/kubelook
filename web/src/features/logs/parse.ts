export const LEVELS = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL'] as const;
export type Level = (typeof LEVELS)[number];

const NAMES: Record<string, Level> = {
  trace: 'TRACE',
  trc: 'TRACE',
  debug: 'DEBUG',
  dbg: 'DEBUG',
  info: 'INFO',
  information: 'INFO',
  notice: 'INFO',
  warn: 'WARN',
  warning: 'WARN',
  err: 'ERROR',
  error: 'ERROR',
  fatal: 'FATAL',
  critical: 'FATAL',
  crit: 'FATAL',
  panic: 'FATAL',
};

/** pino and bunyan use numbers: 10 trace, 20 debug, 30 info, 40 warn, 50 error, 60 fatal (values in between round down). */
function fromNumber(n: number): Level | undefined {
  if (!Number.isFinite(n) || n < 10 || n >= 70) return undefined;
  return LEVELS[Math.floor(n / 10) - 1];
}

/** A log level in any of the usual spellings, as one of the six levels; undefined when it is not one. */
export function normalizeLevel(value: unknown): Level | undefined {
  if (typeof value === 'number') return fromNumber(value);
  if (typeof value !== 'string') return undefined;
  const text = value.trim().toLowerCase();
  if (text === '') return undefined;
  const named = NAMES[text];
  if (named) return named;
  return /^\d+$/.test(text) ? fromNumber(Number(text)) : undefined;
}

export type ParsedLine =
  | { json: false }
  | { json: true; fields: Record<string, unknown>; level?: Level; message?: string; time?: string };

const LEVEL_KEYS = ['level', 'lvl', 'severity', 'loglevel'];
const MESSAGE_KEYS = ['msg', 'message'];
const TIME_KEYS = ['time', 'ts', 'timestamp', '@timestamp'];

function firstLevel(fields: Record<string, unknown>): Level | undefined {
  for (const k of LEVEL_KEYS) {
    if (k in fields) return normalizeLevel(fields[k]);
  }
  return undefined;
}

function firstString(
  fields: Record<string, unknown>,
  keys: string[],
  allowNumber: boolean,
): string | undefined {
  for (const k of keys) {
    const v = fields[k];
    if (typeof v === 'string') return v;
    if (allowNumber && typeof v === 'number') return String(v);
  }
  return undefined;
}

/** Reads a line that is one JSON object into its level, message and time; any other line is plain. */
export function parseLine(text: string): ParsedLine {
  const t = text.trim();
  if (!t.startsWith('{') || !t.endsWith('}')) return { json: false };
  let value: unknown;
  try {
    value = JSON.parse(t);
  } catch {
    return { json: false };
  }
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return { json: false };
  const fields = value as Record<string, unknown>;
  return {
    json: true,
    fields,
    level: firstLevel(fields),
    message: firstString(fields, MESSAGE_KEYS, false),
    time: firstString(fields, TIME_KEYS, true),
  };
}
