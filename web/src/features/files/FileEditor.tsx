import { useState } from 'react';
import { Banner } from '../../components/Banner';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { decodeUtf8, encodeUtf8, humanSize } from '../../lib/bytes';
import { languageFor } from '../../lib/language';
import { CodeEditor } from '../editor/lazyEditors';
import type { FileContent } from './useFiles';

export interface FileEditorProps {
  path: string;
  content: FileContent;
  onSave: (data: Uint8Array) => void;
  onDownload: () => void;
  onClose: () => void;
  saving: boolean;
  error?: string;
  canWrite: boolean;
}

/** A file of the container: editable text, a notice for large files, a download for binary ones. */
export function FileEditor({
  path,
  content,
  onSave,
  onDownload,
  onClose,
  saving,
  error,
  canWrite,
}: FileEditorProps) {
  const original = content.binary ? '' : (decodeUtf8(content.data) ?? '');
  const notText = content.binary || (!content.truncated && decodeUtf8(content.data) === null);
  const [text, setText] = useState(original);
  const readOnly = content.truncated || !canWrite;
  const dirty = text !== original;
  return (
    <section aria-label={`File ${path}`}>
      <h3>{path}</h3>
      <p>{`${humanSize(content.stat.size)}, modified ${content.stat.modified}`}</p>
      {notText ? (
        <>
          <p>Binary file</p>
          <Button onClick={onDownload}>Download</Button>
        </>
      ) : (
        <>
          {content.truncated && (
            <Banner kind="info">
              This file is larger than 1 MiB; only the start is shown, and it is read-only here. Download it
              to see all of it.
            </Banner>
          )}
          <CodeEditor
            label={`Contents of ${path}`}
            value={text}
            onChange={setText}
            language={languageFor(path)}
            readOnly={readOnly}
          />
          <ErrorText message={error} />
          {!readOnly && (
            <Button onClick={() => onSave(encodeUtf8(text))} disabled={!dirty || saving}>
              Save
            </Button>
          )}
          <Button onClick={onDownload}>Download</Button>
        </>
      )}
      <Button onClick={onClose}>Close</Button>
    </section>
  );
}
