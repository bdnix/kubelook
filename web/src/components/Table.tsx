import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import type { Column, Status } from '../kinds/types';
import { getString } from '../lib/path';
import { visibleRange } from '../lib/window';
import type { K8sObject } from '../state/live';
import { StatusBadge } from './StatusBadge';

export interface TableProps {
  label: string;
  columns: Column[];
  rows: K8sObject[];
  /** A stable key per row (the object's uid), so rows are not re-rendered when others change. */
  getKey: (row: K8sObject) => string;
  /** The current time in milliseconds, for age columns. */
  now: number;
  onOpen?: (row: K8sObject) => void;
  /** The health shown as a badge in the `status` column. */
  statusOf?: (row: K8sObject) => Status;
  onSelectionChange?: (keys: string[]) => void;
  /** The height of the scroll area; measured when absent. */
  viewportHeight?: number;
  rowHeight?: number;
}

const OVERSCAN = 5;
const DEFAULT_VIEWPORT = 480;

type Sort = { id: string; dir: 'asc' | 'desc' } | null;

function compare(col: Column, a: K8sObject, b: K8sObject, now: number): number {
  switch (col.sort) {
    case 'number':
      return Number(col.value(a, now)) - Number(col.value(b, now));
    case 'age': {
      const ta = Date.parse(getString(a, 'metadata.creationTimestamp'));
      const tb = Date.parse(getString(b, 'metadata.creationTimestamp'));
      return (Number.isNaN(tb) ? 0 : tb) - (Number.isNaN(ta) ? 0 : ta);
    }
    default:
      return String(col.value(a, now)).localeCompare(String(col.value(b, now)));
  }
}

function useViewportHeight(
  ref: React.RefObject<HTMLDivElement | null>,
  override: number | undefined,
): number {
  const [measured, setMeasured] = useState(DEFAULT_VIEWPORT);
  useEffect(() => {
    const el = ref.current;
    if (override !== undefined || !el || typeof ResizeObserver === 'undefined') return undefined;
    const observer = new ResizeObserver(() => setMeasured(el.clientHeight || DEFAULT_VIEWPORT));
    observer.observe(el);
    return () => observer.disconnect();
  }, [ref, override]);
  return override ?? measured;
}

/** A sortable, filterable, selectable list that renders only the rows in view. */
export function Table({
  label,
  columns,
  rows,
  getKey,
  now,
  onOpen,
  statusOf,
  onSelectionChange,
  viewportHeight,
  rowHeight = 32,
}: TableProps) {
  const [sort, setSort] = useState<Sort>(null);
  const [filter, setFilter] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [anchor, setAnchor] = useState<number | null>(null);
  const [active, setActive] = useState(0);
  const [scrollTop, setScrollTop] = useState(0);
  const scroller = useRef<HTMLDivElement>(null);
  const height = useViewportHeight(scroller, viewportHeight);

  const shown = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    let out = rows;
    if (needle !== '') {
      out = rows.filter((r) => columns.some((c) => String(c.value(r, now)).toLowerCase().includes(needle)));
    }
    const col = sort && columns.find((c) => c.id === sort.id);
    if (sort && col) {
      const sign = sort.dir === 'asc' ? 1 : -1;
      out = [...out].sort((a, b) => sign * compare(col, a, b, now));
    }
    return out;
  }, [rows, columns, filter, sort, now]);

  const [start, end] = visibleRange({
    rowCount: shown.length,
    rowHeight,
    viewportHeight: height,
    scrollTop,
    overscan: OVERSCAN,
  });
  const lastIndex = Math.max(0, shown.length - 1);
  const activeIndex = Math.min(active, lastIndex);

  const changeSelection = (next: Set<string>) => {
    setSelected(next);
    onSelectionChange?.([...next]);
  };

  const toggle = (index: number, shift: boolean) => {
    const row = shown[index];
    if (!row) return;
    const next = new Set(selected);
    if (shift && anchor !== null) {
      const [from, to] = anchor < index ? [anchor, index] : [index, anchor];
      for (let i = from; i <= to; i++) {
        const r = shown[i];
        if (r) next.add(getKey(r));
      }
    } else {
      const k = getKey(row);
      if (next.has(k)) next.delete(k);
      else next.add(k);
      setAnchor(index);
    }
    changeSelection(next);
  };

  const sortBy = (id: string) =>
    setSort((s) => (s?.id !== id ? { id, dir: 'asc' } : s.dir === 'asc' ? { id, dir: 'desc' } : null));

  const moveTo = (index: number) => {
    const i = Math.min(Math.max(index, 0), lastIndex);
    setActive(i);
    const top = i * rowHeight;
    let next = scrollTop;
    if (top < scrollTop) next = top;
    else if (top + rowHeight > scrollTop + height) next = top + rowHeight - height;
    if (next !== scrollTop) {
      setScrollTop(next);
      if (scroller.current) scroller.current.scrollTop = next;
    }
  };

  const onKeyDown = (e: KeyboardEvent) => {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        moveTo(activeIndex + 1);
        break;
      case 'ArrowUp':
        e.preventDefault();
        moveTo(activeIndex - 1);
        break;
      case 'Home':
        e.preventDefault();
        moveTo(0);
        break;
      case 'End':
        e.preventDefault();
        moveTo(lastIndex);
        break;
      case 'Enter': {
        const row = shown[activeIndex];
        if (row && onOpen) onOpen(row);
        break;
      }
      case ' ':
        e.preventDefault();
        toggle(activeIndex, e.shiftKey);
        break;
    }
  };

  const rowId = (i: number) => `${label}-row-${i}`.replace(/\s+/g, '-');
  const cell = (c: Column, row: K8sObject) =>
    c.id === 'status' && statusOf ? <StatusBadge status={statusOf(row)} /> : String(c.value(row, now));

  return (
    <div className="table">
      <label>
        Filter {label}
        <input type="search" value={filter} onChange={(e) => setFilter(e.target.value)} />
      </label>
      <div
        role="grid"
        aria-label={label}
        aria-rowcount={shown.length}
        aria-activedescendant={shown.length > 0 ? rowId(activeIndex) : undefined}
        tabIndex={0}
        onKeyDown={onKeyDown}
        className="grid"
      >
        <div role="row" className="grid-head">
          <span role="columnheader" className="grid-check" />
          {columns.map((c) => (
            <button
              key={c.id}
              type="button"
              role="columnheader"
              aria-sort={sort?.id === c.id ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'}
              onClick={() => sortBy(c.id)}
            >
              {c.title}
            </button>
          ))}
        </div>
        <div
          ref={scroller}
          className="grid-body"
          style={{ height, overflowY: 'auto' }}
          onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)}
        >
          <div style={{ height: shown.length * rowHeight, position: 'relative' }}>
            {shown.slice(start, end).map((row, offset) => {
              const index = start + offset;
              const key = getKey(row);
              return (
                <div
                  key={key}
                  id={rowId(index)}
                  role="row"
                  aria-rowindex={index + 1}
                  aria-selected={selected.has(key)}
                  className={index === activeIndex ? 'grid-row active' : 'grid-row'}
                  style={{
                    position: 'absolute',
                    top: index * rowHeight,
                    height: rowHeight,
                    left: 0,
                    right: 0,
                  }}
                  onClick={() => setActive(index)}
                  onDoubleClick={() => onOpen?.(row)}
                >
                  <span role="gridcell" className="grid-check">
                    <input
                      type="checkbox"
                      aria-label={`Select ${getString(row, 'metadata.name')}`}
                      checked={selected.has(key)}
                      onChange={() => undefined}
                      onClick={(e) => toggle(index, e.shiftKey)}
                    />
                  </span>
                  {columns.map((c) => (
                    <span key={c.id} role="gridcell">
                      {cell(c, row)}
                    </span>
                  ))}
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
