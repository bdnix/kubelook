import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { Field } from '../../components/Field';
import { useAddRepository, useRemoveRepository, useRepositories } from './api';

/** The chart repositories: the organisation's, and the ones added for this session. Passwords are never shown. */
export function RepositoriesPanel({ cluster }: { cluster: string }) {
  const repos = useRepositories(cluster);
  const add = useAddRepository(cluster);
  const remove = useRemoveRepository(cluster);
  const qc = useQueryClient();
  const [form, setForm] = useState({ name: '', url: '', username: '', password: '' });
  const refresh = () => qc.invalidateQueries({ queryKey: ['helm', cluster, 'repositories'] });
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) =>
    setForm((f) => ({ ...f, [k]: e.target.value }));
  return (
    <section aria-label="Chart repositories">
      <h2>Chart repositories</h2>
      <ul aria-label="Repositories">
        {(repos.data ?? []).map((r) => (
          <li key={r.name}>
            <strong>{r.name}</strong> {r.url}{' '}
            <span>{r.scope === 'org' ? '(organisation)' : '(this session)'}</span>
            {r.scope === 'session' && (
              <Button
                aria-label={`Remove ${r.name}`}
                onClick={() => remove.mutate(r.name, { onSuccess: () => void refresh() })}
              >
                Remove
              </Button>
            )}
          </li>
        ))}
      </ul>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          add.mutate(form, {
            onSuccess: () => {
              setForm({ name: '', url: '', username: '', password: '' });
              void refresh();
            },
          });
        }}
      >
        <Field label="Name" value={form.name} onChange={set('name')} />
        <Field label="URL" value={form.url} onChange={set('url')} />
        <Field label="Username" value={form.username} onChange={set('username')} />
        <Field label="Password" type="password" value={form.password} onChange={set('password')} />
        <Button type="submit" disabled={add.isPending || form.name === '' || form.url === ''}>
          Add repository
        </Button>
      </form>
      <ErrorText message={add.error?.message ?? remove.error?.message} />
    </section>
  );
}
