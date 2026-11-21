import { parse } from 'yaml';

export type ParsedValues = { ok: true; value: Record<string, unknown> } | { ok: false; error: string };

/** The values of a release as typed in the editor: a YAML mapping, an empty text meaning no values. */
export function parseValues(text: string): ParsedValues {
  try {
    const v: unknown = parse(text);
    if (v === null || v === undefined) return { ok: true, value: {} };
    if (typeof v !== 'object' || Array.isArray(v))
      return { ok: false, error: 'The values must be a YAML mapping.' };
    return { ok: true, value: v as Record<string, unknown> };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'The values are not valid YAML.' };
  }
}
