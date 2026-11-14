import { useState } from 'react';
import { useSettings } from '../../api/settings';
import { Button } from '../../components/Button';
import { useLive } from '../../state/useLive';

/** Chooses the namespaces a cluster's lists show; the choice is saved per cluster in the user's settings. */
export function NamespaceFilter({ cluster }: { cluster: string }) {
  const { settings, update } = useSettings();
  const [open, setOpen] = useState(false);
  // The names are watched only while the list is open: a closed filter costs no watch.
  const live = useLive(
    open ? { cluster, group: '', version: 'v1', resource: 'namespaces', namespace: '' } : undefined,
  );
  const selected = settings?.namespaceFilters[cluster] ?? [];
  const names = live.items
    .map((o) => String((o.metadata as { name?: unknown } | undefined)?.name ?? ''))
    .filter((n) => n !== '')
    .sort();
  const summary =
    selected.length === 0
      ? 'all'
      : selected.length <= 3
        ? selected.join(', ')
        : `${selected.length} selected`;
  const save = (next: string[]) =>
    void update((s) => {
      const { [cluster]: _old, ...rest } = s.namespaceFilters;
      void _old;
      return { ...s, namespaceFilters: next.length === 0 ? rest : { ...rest, [cluster]: next } };
    });
  const toggle = (name: string) =>
    save(selected.includes(name) ? selected.filter((n) => n !== name) : [...selected, name]);

  return (
    <div className="namespace-filter">
      <Button aria-expanded={open} aria-label={`Namespaces: ${summary}`} onClick={() => setOpen(!open)}>
        {`Namespaces: ${summary}`}
      </Button>
      {open && (
        <div role="group" aria-label="Namespaces">
          {names.map((n) => (
            <label key={n}>
              <input type="checkbox" checked={selected.includes(n)} onChange={() => toggle(n)} />
              {n}
            </label>
          ))}
          <Button disabled={selected.length === 0} onClick={() => save([])}>
            Clear selection
          </Button>
        </div>
      )}
    </div>
  );
}
