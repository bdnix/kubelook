import { useQueryClient } from '@tanstack/react-query';
import { useState, type ChangeEvent, type FormEvent } from 'react';
import { useApi } from '../../api/context';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { Field } from '../../components/Field';
import { useVaultStore } from './context';
import { errorText } from './errors';

function baseName(fileName: string): string {
  const dot = fileName.lastIndexOf('.');
  return dot > 0 ? fileName.slice(0, dot) : fileName;
}

/** Adds a kubeconfig from a file or pasted text. The server's rejection message is shown as it is. */
export function KubeconfigForm() {
  const api = useApi();
  const store = useVaultStore();
  const queryClient = useQueryClient();
  const [label, setLabel] = useState('');
  const [content, setContent] = useState('');
  const [fileKey, setFileKey] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function onFile(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    setContent(await file.text());
    setLabel((current) => (current === '' ? baseName(file.name) : current));
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      const { sealed } = await api.post<{ sealed: string }>('/credentials/kubeconfig', { label, content });
      await store.put({
        id: `kubeconfig:${label}`,
        kind: 'kubeconfig',
        label,
        sealed,
        savedAt: new Date(Date.now()).toISOString(),
      });
      setLabel('');
      setContent('');
      setFileKey((k) => k + 1);
      await queryClient.invalidateQueries({ queryKey: ['clusters'] });
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={(e) => void submit(e)} aria-label="Add a kubeconfig">
      <Field key={fileKey} label="Kubeconfig file" type="file" onChange={(e) => void onFile(e)} />
      <Field
        label="Label"
        value={label}
        onChange={(e) => setLabel(e.target.value)}
        hint="Shown in the cluster list."
      />
      <Field
        label="Kubeconfig"
        multiline
        rows={8}
        value={content}
        onChange={(e) => setContent(e.target.value)}
        hint="Credentials must be inside the file as data. Exec plugins and file paths are not supported."
      />
      <ErrorText message={error} />
      <Button type="submit" disabled={busy || label === '' || content === ''}>
        Add kubeconfig
      </Button>
    </form>
  );
}
