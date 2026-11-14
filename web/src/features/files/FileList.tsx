import { useState } from 'react';
import { Button } from '../../components/Button';
import { humanSize } from '../../lib/bytes';
import type { FileEntry } from './useFiles';

const TYPES: Record<FileEntry['type'], { icon: string; label: string }> = {
  file: { icon: '📄', label: 'File' },
  dir: { icon: '📁', label: 'Directory' },
  link: { icon: '🔗', label: 'Link' },
  other: { icon: '⚙', label: 'Special file' },
};

export interface FileListProps {
  entries: FileEntry[];
  onOpen: (entry: FileEntry) => void;
  onDownload: (entry: FileEntry) => void;
  onDelete: (entry: FileEntry) => void;
}

/** The entries of a directory, with a filter box. Cluster text is shown as text only. */
export function FileList({ entries, onOpen, onDownload, onDelete }: FileListProps) {
  const [filter, setFilter] = useState('');
  const needle = filter.trim().toLowerCase();
  const shown = needle === '' ? entries : entries.filter((e) => e.name.toLowerCase().includes(needle));
  return (
    <div>
      <input
        type="search"
        aria-label="Filter files"
        placeholder="Filter files"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
      />
      <table aria-label="Files">
        <thead>
          <tr>
            <th>Name</th>
            <th>Mode</th>
            <th>Size</th>
            <th>Modified</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {shown.map((e) => {
            const kind = TYPES[e.type];
            return (
              <tr key={e.name}>
                <td>
                  <span role="img" aria-label={kind.label}>
                    {kind.icon}
                  </span>{' '}
                  <button type="button" onClick={() => onOpen(e)}>
                    {e.name}
                  </button>
                  {e.target !== undefined && <span>{` → ${e.target}`}</span>}
                </td>
                <td>{e.mode}</td>
                <td>{e.type === 'dir' ? '' : humanSize(e.size)}</td>
                <td>{e.modified}</td>
                <td>
                  <Button onClick={() => onDownload(e)} aria-label={`Download ${e.name}`}>
                    Download
                  </Button>{' '}
                  <Button onClick={() => onDelete(e)} aria-label={`Delete ${e.name}`}>
                    Delete
                  </Button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {shown.length === 0 && (
        <p>{entries.length === 0 ? 'This directory is empty.' : 'No file matches the filter.'}</p>
      )}
    </div>
  );
}
