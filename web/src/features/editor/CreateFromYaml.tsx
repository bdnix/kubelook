import { useState } from 'react';
import { ApiError } from '../../api/client';
import { useApi } from '../../api/context';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { splitDocuments } from '../../lib/yaml';
import { YamlEditor } from './lazyEditors';

interface ApplyResult {
  index: number;
  kind: string;
  namespace: string;
  name: string;
  action?: 'created' | 'updated';
  error?: string;
}

type Phase = 'idle' | 'running';

const TEMPLATE =
  'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\n  namespace: default\ndata:\n  key: value\n';

/**
 * Creates objects from YAML (several documents allowed). A dry run goes first; only when every document passes
 * is the real request sent. The result of each document is listed.
 */
export function CreateFromYaml({ cluster }: { cluster: string }) {
  const api = useApi();
  const [text, setText] = useState(TEMPLATE);
  const [phase, setPhase] = useState<Phase>('idle');
  const [results, setResults] = useState<ApplyResult[]>([]);
  const [dryRun, setDryRun] = useState(false);
  const [error, setError] = useState<string>();
  const documents = splitDocuments(text);

  const run = async (create: boolean) => {
    setPhase('running');
    setError(undefined);
    const path = `/clusters/${encodeURIComponent(cluster)}/apply`;
    try {
      let r = (await api.post<{ results: ApplyResult[] }>(path, { documents, dryRun: true })).results;
      let preview = true;
      if (create && r.every((x) => x.error === undefined)) {
        r = (await api.post<{ results: ApplyResult[] }>(path, { documents, dryRun: false })).results;
        preview = false;
      }
      setResults(r);
      setDryRun(preview);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Something went wrong.');
    } finally {
      setPhase('idle');
    }
  };

  const disabled = phase === 'running' || documents.length === 0;
  return (
    <section aria-label="Create from YAML">
      <h2>Create from YAML</h2>
      <YamlEditor label="YAML to create" value={text} onChange={setText} />
      <ErrorText message={error} />
      <div>
        <Button onClick={() => void run(false)} disabled={disabled}>
          Preview
        </Button>
        <Button onClick={() => void run(true)} disabled={disabled}>
          Create
        </Button>
      </div>
      {results.length > 0 && (
        <ul aria-label="Results">
          {results.map((r) => (
            <li key={`${r.index}-${r.kind}-${r.name}`}>
              {r.error !== undefined
                ? `Document ${r.index + 1}${r.kind ? ` (${r.kind}${r.name ? ` ${r.name}` : ''})` : ''}: ${r.error}`
                : `${r.kind} ${r.namespace ? `${r.namespace}/` : ''}${r.name}: ${dryRun ? `would be ${r.action ?? ''}` : (r.action ?? '')}`}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
