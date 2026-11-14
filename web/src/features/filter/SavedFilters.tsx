import { useState } from 'react';
import { Button } from '../../components/Button';

export interface FilterSet {
  name: string;
  kind: string;
  query: string;
}

export interface SavedFiltersProps {
  kind: string;
  sets: FilterSet[];
  /** The query now in the bar: what "Save filter" stores. */
  query: string;
  onLoad: (query: string) => void;
  onSave: (set: FilterSet) => void;
  onRename: (oldName: string, newName: string) => void;
  onDelete: (name: string) => void;
}

/** The user's saved filter sets of one kind: load, save the current query, rename and delete. */
export function SavedFilters({ kind, sets, query, onLoad, onSave, onRename, onDelete }: SavedFiltersProps) {
  const mine = sets.filter((s) => s.kind === kind);
  const [name, setName] = useState('');
  const [renaming, setRenaming] = useState<string>();
  const [newName, setNewName] = useState('');
  const taken = (n: string, except?: string) => mine.some((s) => s.name === n.trim() && s.name !== except);
  const clash = name.trim() !== '' && taken(name);
  return (
    <div className="saved-filters">
      {mine.length > 0 && (
        <ul aria-label="Saved filters">
          {mine.map((s) => (
            <li key={s.name}>
              {renaming === s.name ? (
                <>
                  <label>
                    New name
                    <input value={newName} onChange={(e) => setNewName(e.target.value)} />
                  </label>
                  <Button
                    disabled={newName.trim() === '' || taken(newName, s.name)}
                    onClick={() => {
                      onRename(s.name, newName.trim());
                      setRenaming(undefined);
                    }}
                  >
                    Rename
                  </Button>
                  <Button onClick={() => setRenaming(undefined)}>Cancel</Button>
                </>
              ) : (
                <>
                  <Button aria-label={`Load ${s.name}`} onClick={() => onLoad(s.query)}>
                    {s.name}
                  </Button>
                  <Button
                    aria-label={`Rename ${s.name}`}
                    onClick={() => {
                      setRenaming(s.name);
                      setNewName(s.name);
                    }}
                  >
                    Rename
                  </Button>
                  <Button aria-label={`Delete ${s.name}`} onClick={() => onDelete(s.name)}>
                    Delete
                  </Button>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
      <label>
        Name
        <input value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      {clash && <p role="status">A filter with this name exists.</p>}
      <Button
        disabled={query.trim() === '' || name.trim() === '' || clash}
        onClick={() => {
          onSave({ name: name.trim(), kind, query });
          setName('');
        }}
      >
        Save filter
      </Button>
    </div>
  );
}
