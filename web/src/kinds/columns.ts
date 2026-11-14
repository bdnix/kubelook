import { formatAge } from '../lib/age';
import { get, getArray, getString } from '../lib/path';
import type { K8sObject } from '../state/live';
import type { Column, KindDescriptor } from './types';

export const nameColumn: Column = { id: 'name', title: 'Name', value: (o) => getString(o, 'metadata.name') };
export const namespaceColumn: Column = {
  id: 'namespace',
  title: 'Namespace',
  value: (o) => getString(o, 'metadata.namespace'),
};
export const ageColumn: Column = {
  id: 'age',
  title: 'Age',
  value: (o, now) => formatAge(getString(o, 'metadata.creationTimestamp'), now),
  sort: 'age',
};

/** A text column read from a dotted path; numbers and booleans are shown as text. */
export function pathColumn(id: string, title: string, path: string): Column {
  return {
    id,
    title,
    value: (o) => {
      const v = get(o, path);
      return typeof v === 'string' ? v : typeof v === 'number' || typeof v === 'boolean' ? String(v) : '';
    },
  };
}

/** A numeric column read from a dotted path; a missing value counts as 0. */
export function numberColumn(id: string, title: string, path: string): Column {
  return {
    id,
    title,
    value: (o) => {
      const v = get(o, path);
      return typeof v === 'number' ? v : 0;
    },
    sort: 'number',
  };
}

/** The number of entries of the array at `path`, or of the keys of the object at `path`. */
export function countOf(o: K8sObject, path: string): number {
  const v = get(o, path);
  if (Array.isArray(v)) return v.length;
  return v !== null && typeof v === 'object' ? Object.keys(v).length : 0;
}

export function countColumn(id: string, title: string, path: string): Column {
  return { id, title, value: (o) => countOf(o, path), sort: 'number' };
}

/** The `status` of the condition of `type` (`True`, `False`, `Unknown`), empty when the object has none. */
export function conditionStatus(type: string, path = 'status.conditions'): (o: K8sObject) => string {
  return (o) => {
    const c = getArray(o, path).find((x) => getString(x, 'type') === type);
    return c ? getString(c, 'status') : '';
  };
}

function metaMap(o: K8sObject, field: 'labels' | 'annotations'): Record<string, unknown> {
  const m = get(o, `metadata.${field}`);
  return m !== null && typeof m === 'object' ? (m as Record<string, unknown>) : {};
}

/** A label value; keys contain dots, so they cannot be read with a dotted path. */
export function labelValue(o: K8sObject, key: string): string {
  const v = metaMap(o, 'labels')[key];
  return typeof v === 'string' ? v : '';
}

export function annotationValue(o: K8sObject, key: string): string {
  const v = metaMap(o, 'annotations')[key];
  return typeof v === 'string' ? v : '';
}

/** `key=value` pairs, sorted, comma separated. */
export function labelString(labels: unknown): string {
  if (labels === null || typeof labels !== 'object') return '';
  return Object.entries(labels as Record<string, unknown>)
    .map(([k, v]) => `${k}=${String(v)}`)
    .sort()
    .join(',');
}

export interface KindSpec extends Omit<KindDescriptor, 'columns'> {
  /** The columns between Name/Namespace and Age. */
  columns: Column[];
}

/** A descriptor with Name, Namespace (when namespaced) first and Age last. */
export function defineKind(spec: KindSpec): KindDescriptor {
  return {
    ...spec,
    columns: [nameColumn, ...(spec.namespaced ? [namespaceColumn] : []), ...spec.columns, ageColumn],
  };
}
