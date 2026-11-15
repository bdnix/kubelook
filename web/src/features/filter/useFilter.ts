import { useMemo } from 'react';
import { compileFilter } from '../../lib/filter/evaluate';
import { formatTerm, parseFilter, type Filter, type ParseError } from '../../lib/filter/parse';
import { secretViolation } from '../../lib/filter/paths';
import type { K8sObject } from '../../state/live';

export interface FilterResult {
  /** The rows that match; the list itself while the filter is empty or cannot be applied. */
  rows: K8sObject[];
  filter: Filter;
  error?: ParseError;
  /** Regular-expression conditions that do not compile, as text; they filter nothing. */
  invalid: string[];
  /** Set when the filter reads Secret values: it is not applied. */
  secretError?: string;
}

/**
 * Applies the query to the list. The query is parsed when it changes, and the rows are evaluated when the list or
 * the filter changes, not on every render.
 */
export function useFilter(
  items: K8sObject[],
  query: string,
  kind: { group: string; kind: string },
  now: number,
): FilterResult {
  const parsed = useMemo(
    () => (query.trim() === '' ? { ok: true as const, filter: [] as Filter } : parseFilter(query)),
    [query],
  );
  const { group, kind: kindName } = kind;
  return useMemo(() => {
    if (!parsed.ok) return { rows: items, filter: [], error: parsed.error, invalid: [] };
    const secretError = secretViolation(parsed.filter, { group, kind: kindName });
    if (secretError) return { rows: items, filter: parsed.filter, invalid: [], secretError };
    if (parsed.filter.length === 0) return { rows: items, filter: parsed.filter, invalid: [] };
    const compiled = compileFilter(parsed.filter, now);
    return {
      rows: items.filter((o) => compiled.test(o)),
      filter: parsed.filter,
      invalid: compiled.invalid.map(formatTerm),
    };
  }, [items, parsed, group, kindName, now]);
}
