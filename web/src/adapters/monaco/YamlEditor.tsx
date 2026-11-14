import Editor from '@monaco-editor/react';
import { useEffect } from 'react';
import { setYamlSchema } from './setup';

export interface YamlEditorProps {
  value: string;
  onChange: (value: string) => void;
  readOnly?: boolean;
  /** The OpenAPI v3 document and the `$ref` of the kind, for completion and validation. */
  schema?: unknown;
  schemaRef?: string;
  label: string;
}

/** The YAML editor. It only wires props to Monaco; everything testable lives outside this folder. */
export function YamlEditor({ value, onChange, readOnly = false, schema, schemaRef, label }: YamlEditorProps) {
  useEffect(() => setYamlSchema(schema, schemaRef), [schema, schemaRef]);
  return (
    <Editor
      height="60vh"
      language="yaml"
      value={value}
      onChange={(v) => onChange(v ?? '')}
      options={{ readOnly, ariaLabel: label, minimap: { enabled: false }, scrollBeyondLastLine: false }}
    />
  );
}
