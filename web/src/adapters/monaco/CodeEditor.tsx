import Editor from '@monaco-editor/react';
import './setup';

export interface CodeEditorProps {
  value: string;
  onChange: (value: string) => void;
  language: string;
  readOnly?: boolean;
  label: string;
}

/** A plain code editor for files. It only wires props to Monaco. */
export function CodeEditor({ value, onChange, language, readOnly = false, label }: CodeEditorProps) {
  return (
    <Editor
      height="50vh"
      language={language}
      value={value}
      onChange={(v) => onChange(v ?? '')}
      options={{ readOnly, ariaLabel: label, minimap: { enabled: false }, scrollBeyondLastLine: false }}
    />
  );
}
