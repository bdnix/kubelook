import { useState } from 'react';
import { stringify } from 'yaml';
import { Button } from '../../components/Button';
import type { Filter } from '../../lib/filter/parse';
import { toKubectl } from '../../lib/filter/toJq';
import { browserSaveFile, type SaveFile } from '../../lib/saveFile';
import type { K8sObject } from '../../state/live';
import type { Clipboard } from '../actions/ConfirmDialog';

const browserClipboard: Clipboard = { writeText: (t) => navigator.clipboard.writeText(t) };

const stamp = (d: Date) => d.toISOString().replace(/[-:]/g, '').replace('T', '-').slice(0, 15);

export interface DownloadMenuProps {
  cluster: string;
  kind: string;
  /** The resource name kubectl uses, such as `pods`. */
  resource: string;
  /** The namespace of the list; the empty string for all namespaces; null for a cluster-scoped kind. */
  namespace: string | null;
  /** The objects that match the filter, already redacted. */
  objects: K8sObject[];
  filter: Filter;
  saveFile?: SaveFile;
  clipboard?: Clipboard;
  now?: () => Date;
}

/** Downloads the matching objects as YAML or JSON, or copies the kubectl command that lists them. */
export function DownloadMenu({
  cluster,
  resource,
  namespace,
  objects,
  filter,
  saveFile = browserSaveFile,
  clipboard = browserClipboard,
  now = () => new Date(),
}: DownloadMenuProps) {
  const [open, setOpen] = useState(false);
  const save = (ext: 'yaml' | 'json') => {
    const text =
      ext === 'yaml'
        ? objects.map((o) => stringify(o, { lineWidth: 0 })).join('---\n')
        : JSON.stringify({ apiVersion: 'v1', kind: 'List', items: objects }, null, 2) + '\n';
    saveFile(`${cluster}-${resource}-${stamp(now())}.${ext}`, new TextEncoder().encode(text));
    setOpen(false);
  };
  const copy = (tool: 'jq' | 'yq') => {
    void clipboard.writeText(toKubectl(filter, { resource, namespace }, tool));
    setOpen(false);
  };
  return (
    <div className="download-menu">
      <Button
        aria-haspopup="menu"
        aria-expanded={open}
        disabled={objects.length === 0}
        onClick={() => setOpen(!open)}
      >
        Download matches
      </Button>
      {open && (
        <div role="menu" aria-label="Download matches">
          <button type="button" role="menuitem" onClick={() => save('yaml')}>
            YAML
          </button>
          <button type="button" role="menuitem" onClick={() => save('json')}>
            JSON
          </button>
          <button type="button" role="menuitem" onClick={() => copy('jq')}>
            Copy as kubectl | jq
          </button>
          <button type="button" role="menuitem" onClick={() => copy('yq')}>
            Copy as kubectl | yq
          </button>
        </div>
      )}
    </div>
  );
}
