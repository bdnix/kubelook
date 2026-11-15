import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { useFocusTrap } from '../../lib/useFocusTrap';
import { fuzzyScore, usePaletteItems, type PaletteItem } from './usePaletteItems';

function Palette({ items, onClose }: { items: PaletteItem[]; onClose: () => void }) {
  const navigate = useNavigate();
  const listId = useId();
  const ref = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  useFocusTrap(ref);
  useEffect(() => input.current?.focus(), []);

  const shown = useMemo(
    () =>
      items
        .map((item) => ({ item, score: fuzzyScore(query, item.title) }))
        .filter((r) => r.score >= 0)
        .sort((a, b) => b.score - a.score)
        .map((r) => r.item),
    [items, query],
  );
  const current = Math.min(active, Math.max(shown.length - 1, 0));
  const go = (item: PaletteItem | undefined) => {
    if (!item) return;
    void navigate(item.path);
    onClose();
  };

  return (
    <div
      ref={ref}
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.stopPropagation();
          onClose();
        }
      }}
    >
      <input
        ref={input}
        role="combobox"
        aria-expanded="true"
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={shown[current] ? `${listId}-${current}` : undefined}
        aria-label="Go to"
        value={query}
        onChange={(e) => {
          setQuery(e.target.value);
          setActive(0);
        }}
        onKeyDown={(e) => {
          if (shown.length === 0) return;
          if (e.key === 'ArrowDown') {
            e.preventDefault();
            setActive((current + 1) % shown.length);
          } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            setActive((current - 1 + shown.length) % shown.length);
          } else if (e.key === 'Enter') {
            e.preventDefault();
            go(shown[current]);
          }
        }}
      />
      <ul id={listId} role="listbox" aria-label="Results">
        {shown.map((item, i) => (
          <li
            key={item.id}
            id={`${listId}-${i}`}
            role="option"
            aria-selected={i === current}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => go(item)}
          >
            {item.title}
          </li>
        ))}
      </ul>
      {shown.length === 0 && <p>Nothing matches.</p>}
    </div>
  );
}

/** The command palette: Ctrl+K or Cmd+K opens a list of the views and kinds of the open cluster. */
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const items = usePaletteItems();
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setOpen(true);
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, []);
  return open ? <Palette items={items} onClose={() => setOpen(false)} /> : null;
}
