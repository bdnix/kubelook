import { useId, useMemo, useState } from 'react';
import { Button } from '../../components/Button';
import { compileFilter } from '../../lib/filter/evaluate';
import {
  filterToRows,
  formatFilter,
  formatTerm,
  parseFilter,
  rowsToFilter,
  type Filter,
  type Row,
} from '../../lib/filter/parse';
import { collectPaths, secretViolation } from '../../lib/filter/paths';
import type { K8sObject } from '../../state/live';
import { FilterRows } from './FilterRows';

export interface FilterBarProps {
  kind: { group: string; kind: string };
  /** The live objects of the list, for the path suggestions. */
  objects: K8sObject[];
  value: string;
  onChange: (query: string) => void;
}

/** The query bar of a list: type a filter, or edit it as rows. Problems are shown as text beside it. */
export function FilterBar({ kind, objects, value, onChange }: FilterBarProps) {
  const listId = useId();
  const [mode, setMode] = useState<'query' | 'rows'>('query');
  const [rows, setRows] = useState<Row[]>([]);
  const parsed = useMemo(
    () => (value.trim() === '' ? { ok: true as const, filter: [] as Filter } : parseFilter(value)),
    [value],
  );
  const paths = useMemo(() => collectPaths(objects, kind), [objects, kind]);
  const secretError = parsed.ok ? secretViolation(parsed.filter, kind) : undefined;
  const invalid = useMemo(
    () => (parsed.ok ? compileFilter(parsed.filter, 0).invalid.map(formatTerm) : []),
    [parsed],
  );

  const editRows = (next: Row[]) => {
    setRows(next);
    onChange(formatFilter(rowsToFilter(next)));
  };
  const startRows = () => {
    setRows(
      parsed.ok && parsed.filter.length > 0
        ? filterToRows(parsed.filter)
        : [{ join: 'AND', path: '', op: '=', value: '' }],
    );
    setMode('rows');
  };

  return (
    <div className="filter-bar">
      <datalist id={listId}>
        {paths.map((p) => (
          <option key={p} value={p} />
        ))}
      </datalist>
      {mode === 'query' ? (
        <label>
          Field filter
          <input
            type="text"
            value={value}
            onChange={(e) => onChange(e.target.value)}
            list={listId}
            placeholder="status.phase = Running AND spec.nodeName contains worker"
            spellCheck={false}
          />
        </label>
      ) : (
        <FilterRows rows={rows} listId={listId} onChange={editRows} />
      )}
      {value !== '' && <Button onClick={() => onChange('')}>Clear filter</Button>}
      {mode === 'query' ? (
        <Button disabled={!parsed.ok} onClick={startRows}>
          Edit as rows
        </Button>
      ) : (
        <Button onClick={() => setMode('query')}>Edit as query</Button>
      )}
      {!parsed.ok && (
        <p role="alert">{`${parsed.error.message} (at character ${parsed.error.position + 1})`}</p>
      )}
      {secretError && <p role="alert">{secretError}</p>}
      {invalid.length > 0 && (
        <p role="status">{`Invalid regular expression: ${invalid.join('; ')} — the list is not filtered by it.`}</p>
      )}
    </div>
  );
}
