import { useState } from 'react';
import { Button } from '../../components/Button';
import { LEVELS, type LineFilter } from './levels';

export interface FieldFilterProps {
  value: LineFilter;
  onChange: (value: LineFilter) => void;
}

/** Filters the lines of a structured log: by level, and by the value of one field. */
export function FieldFilter({ value, onChange }: FieldFilterProps) {
  const [key, setKey] = useState(value.field?.key ?? '');
  const [fieldValue, setFieldValue] = useState(value.field?.value ?? '');

  const toggle = (level: string) => {
    const next = new Set(value.levels ?? LEVELS);
    if (next.has(level)) next.delete(level);
    else next.add(level);
    const { levels: _old, ...rest } = value;
    void _old;
    onChange(next.size === LEVELS.length ? rest : { ...rest, levels: next });
  };
  const setField = (k: string, v: string) => {
    setKey(k);
    setFieldValue(v);
    onChange({ ...value, field: { key: k, value: v } });
  };
  const clear = () => {
    setKey('');
    setFieldValue('');
    const { field: _old, ...rest } = value;
    void _old;
    onChange(rest);
  };
  return (
    <fieldset className="field-filter">
      <legend>Filter structured lines</legend>
      {LEVELS.map((level) => (
        <label key={level}>
          <input
            type="checkbox"
            checked={!value.levels || value.levels.has(level)}
            onChange={() => toggle(level)}
          />{' '}
          {level}
        </label>
      ))}
      <label>
        Field
        <input value={key} onChange={(e) => setField(e.target.value, fieldValue)} />
      </label>
      <label>
        Value
        <input value={fieldValue} onChange={(e) => setField(key, e.target.value)} />
      </label>
      {key !== '' && (
        <>
          <small>Plain lines are hidden while a field filter is on.</small>
          <Button onClick={clear}>Clear field filter</Button>
        </>
      )}
    </fieldset>
  );
}
