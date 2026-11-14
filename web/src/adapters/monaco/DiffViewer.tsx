import { DiffEditor } from '@monaco-editor/react';
import './setup';

export interface DiffViewerProps {
  original: string;
  modified: string;
  label: string;
}

/** Side-by-side diff, read only. */
export function DiffViewer({ original, modified, label }: DiffViewerProps) {
  return (
    <DiffEditor
      height="60vh"
      language="yaml"
      original={original}
      modified={modified}
      options={{ readOnly: true, ariaLabel: label, renderSideBySide: true, minimap: { enabled: false } }}
    />
  );
}
