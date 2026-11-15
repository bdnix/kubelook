import { useState } from 'react';
import { Button } from '../../components/Button';
import type { Column } from '../../kinds/types';
import { toCsv } from '../../lib/csv';
import { browserSaveFile, type SaveFile } from '../../lib/saveFile';
import type { K8sObject } from '../../state/live';

const pad = (n: number) => String(n).padStart(2, '0');

/** `<cluster>-<kind>-<yyyyMMdd-HHmmss>.<ext>` in UTC; characters that do not belong in a file name become `_`. */
export function exportFileName(cluster: string, kind: string, at: Date, ext: 'csv' | 'json'): string {
  const safe = (s: string) => s.replace(/[^A-Za-z0-9._-]/g, '_');
  const stamp =
    `${at.getUTCFullYear()}${pad(at.getUTCMonth() + 1)}${pad(at.getUTCDate())}` +
    `-${pad(at.getUTCHours())}${pad(at.getUTCMinutes())}${pad(at.getUTCSeconds())}`;
  return `${safe(cluster)}-${safe(kind)}-${stamp}.${ext}`;
}

export interface ExportMenuProps {
  cluster: string;
  /** What the list is, in the file name: `pods`. */
  kind: string;
  /** The visible columns, in order. */
  columns: Column[];
  /** The rows as the list shows them: filtered and sorted. They are already redacted. */
  rows: K8sObject[];
  /** The current time in milliseconds, for age columns. */
  now: number;
  saveFile?: SaveFile;
  clock?: () => Date;
}

/** Exports the list as CSV (the visible columns) or JSON (the objects). Sensitive values are masked upstream. */
export function ExportMenu({
  cluster,
  kind,
  columns,
  rows,
  now,
  saveFile = browserSaveFile,
  clock = () => new Date(),
}: ExportMenuProps) {
  const [open, setOpen] = useState(false);
  const save = (ext: 'csv' | 'json') => {
    const text =
      ext === 'csv'
        ? toCsv(
            columns.map((c) => c.title),
            rows.map((r) => columns.map((c) => c.value(r, now))),
          )
        : JSON.stringify({ apiVersion: 'v1', kind: 'List', items: rows }, null, 2) + '\n';
    saveFile(exportFileName(cluster, kind, clock(), ext), new TextEncoder().encode(text));
    setOpen(false);
  };
  return (
    <div className="export-menu">
      <Button
        aria-haspopup="menu"
        aria-expanded={open}
        disabled={rows.length === 0}
        onClick={() => setOpen(!open)}
      >
        Export
      </Button>
      {open && (
        <div role="menu" aria-label="Export">
          <button type="button" role="menuitem" onClick={() => save('csv')}>
            CSV
          </button>
          <button type="button" role="menuitem" onClick={() => save('json')}>
            JSON
          </button>
        </div>
      )}
    </div>
  );
}
