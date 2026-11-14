import { parse, parseAllDocuments, stringify } from 'yaml';
import type { K8sObject } from '../state/live';

/** An object as YAML, for display and editing: `status` and `metadata.managedFields` are left out. */
export function toYaml(obj: K8sObject, { withStatus = false }: { withStatus?: boolean } = {}): string {
  const copy: K8sObject = { ...obj };
  if (!withStatus) delete copy.status;
  const meta = copy.metadata;
  if (meta !== null && typeof meta === 'object') {
    const { managedFields: _managedFields, ...rest } = meta as Record<string, unknown>;
    void _managedFields;
    copy.metadata = rest;
  }
  return stringify(copy, { lineWidth: 0 });
}

export type ParseResult = { ok: true; value: K8sObject } | { ok: false; error: string };

/** One YAML (or JSON) document that must be a mapping. */
export function parseObject(text: string): ParseResult {
  try {
    const value: unknown = parse(text);
    if (value === null || typeof value !== 'object' || Array.isArray(value)) {
      return { ok: false, error: 'The document must be a mapping with apiVersion, kind and metadata.' };
    }
    return { ok: true, value: value as K8sObject };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'The text is not valid YAML.' };
  }
}

/** Splits multi-document YAML into the text of each non-empty document. */
export function splitDocuments(text: string): string[] {
  return parseAllDocuments(text)
    .map((d) => d.toJS() as unknown)
    .filter((v) => v !== null && v !== undefined)
    .map((v) => stringify(v, { lineWidth: 0 }));
}

/** An object as YAML text, exactly as given (nothing is left out). */
export function stringifyObject(obj: K8sObject): string {
  return stringify(obj, { lineWidth: 0 });
}
