// Monaco is bundled, never loaded from a CDN (the CSP allows only 'self' and blob: workers).
import { loader } from '@monaco-editor/react';
import * as monaco from 'monaco-editor';
import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker.js?worker';
import { configureMonacoYaml, type MonacoYaml } from 'monaco-yaml';
import yamlWorker from 'monaco-yaml/yaml.worker?worker';

(globalThis as { MonacoEnvironment?: unknown }).MonacoEnvironment = {
  getWorker: (_id: string, label: string) => (label === 'yaml' ? new yamlWorker() : new editorWorker()),
};
loader.config({ monaco });

let yaml: MonacoYaml | undefined;

/** Gives the YAML language the schema of the kind being edited. */
export function setYamlSchema(schema: unknown, schemaRef: string | undefined): void {
  const schemas =
    schema && schemaRef
      ? [
          {
            uri: 'kubelook://kind.json',
            fileMatch: ['*'],
            schema: { ...(schema as object), $ref: schemaRef },
          },
        ]
      : [];
  const options = { enableSchemaRequest: false, validate: true, completion: true, hover: true, schemas };
  if (yaml) void yaml.update(options);
  else yaml = configureMonacoYaml(monaco, options);
}
