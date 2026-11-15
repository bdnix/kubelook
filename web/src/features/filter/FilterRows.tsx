import { Button } from '../../components/Button';
import type { Operator, Row } from '../../lib/filter/parse';

const OPERATORS: Operator[] = ['=', '!=', 'contains', 'regex', 'in', 'exists', 'older than', 'newer than'];

export interface FilterRowsProps {
  rows: Row[];
  /** The id of a `<datalist>` with the paths to suggest. */
  listId: string;
  onChange: (rows: Row[]) => void;
}

const asList = (r: Row) => (r.values ?? []).join(', ');

/** The filter as rows of field, operator and value, each joined to the one before with AND or OR. */
export function FilterRows({ rows, listId, onChange }: FilterRowsProps) {
  const set = (i: number, patch: Partial<Row>) =>
    onChange(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  return (
    <div className="filter-rows">
      {rows.map((r, i) => (
        <fieldset key={i} aria-label={`Condition ${i + 1}`}>
          {i > 0 && (
            <label>
              Join
              <select value={r.join} onChange={(e) => set(i, { join: e.target.value as Row['join'] })}>
                <option value="AND">AND</option>
                <option value="OR">OR</option>
              </select>
            </label>
          )}
          <label>
            Field
            <input list={listId} value={r.path} onChange={(e) => set(i, { path: e.target.value })} />
          </label>
          <label>
            Operator
            <select
              value={r.op}
              onChange={(e) => {
                const op = e.target.value as Operator;
                set(
                  i,
                  op === 'in'
                    ? { op, values: r.value ? [r.value] : (r.values ?? []), value: undefined }
                    : { op, value: r.value ?? (r.values ?? []).join(', ') },
                );
              }}
            >
              {OPERATORS.map((op) => (
                <option key={op} value={op}>
                  {op}
                </option>
              ))}
            </select>
          </label>
          {r.op !== 'exists' && (
            <label>
              Value
              <input
                value={r.op === 'in' ? asList(r) : (r.value ?? '')}
                onChange={(e) =>
                  set(
                    i,
                    r.op === 'in'
                      ? {
                          values: e.target.value
                            .split(',')
                            .map((v) => v.trim())
                            .filter((v) => v !== ''),
                        }
                      : { value: e.target.value },
                  )
                }
              />
            </label>
          )}
          <Button
            aria-label={`Remove condition ${i + 1}`}
            onClick={() => onChange(rows.filter((_, j) => j !== i))}
          >
            Remove
          </Button>
        </fieldset>
      ))}
      <Button onClick={() => onChange([...rows, { join: 'AND', path: '', op: '=', value: '' }])}>
        Add condition
      </Button>
    </div>
  );
}
