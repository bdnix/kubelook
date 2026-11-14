import type { K8sObject } from '../state/live';

export type Severity = 'ok' | 'warning' | 'error' | 'unknown';

export interface Status {
  text: string;
  severity: Severity;
}

export interface Column {
  id: string;
  title: string;
  /** The cell's value; `now` is the current time in milliseconds (for ages). */
  value: (obj: K8sObject, now: number) => string | number;
  /** How the column sorts: text (default), numerically, or by creation time (newest first when ascending). */
  sort?: 'text' | 'number' | 'age';
}

export interface KindDescriptor {
  group: string;
  kind: string;
  resource: string;
  namespaced: boolean;
  title: string;
  section: string;
  columns: Column[];
  /** The kind's health, shown as a badge in the column with this id. */
  status?: (obj: K8sObject) => Status;
  actions?: string[];
  /** A note shown above the list (for example a deprecation). */
  note?: string;
}
