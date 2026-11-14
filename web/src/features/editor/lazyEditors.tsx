import { lazy, Suspense } from 'react';
import type { DiffViewerProps } from '../../adapters/monaco/DiffViewer';
import type { YamlEditorProps } from '../../adapters/monaco/YamlEditor';

// Monaco is large: it is loaded when an editor is first shown, not with the app.
const YamlEditorLazy = lazy(() =>
  import('../../adapters/monaco/YamlEditor').then((m) => ({ default: m.YamlEditor })),
);
const DiffViewerLazy = lazy(() =>
  import('../../adapters/monaco/DiffViewer').then((m) => ({ default: m.DiffViewer })),
);

const fallback = <p role="status">Loading the editor…</p>;

export function YamlEditor(props: YamlEditorProps) {
  return (
    <Suspense fallback={fallback}>
      <YamlEditorLazy {...props} />
    </Suspense>
  );
}

export function DiffViewer(props: DiffViewerProps) {
  return (
    <Suspense fallback={fallback}>
      <DiffViewerLazy {...props} />
    </Suspense>
  );
}
