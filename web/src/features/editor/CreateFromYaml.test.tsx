import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, renderApp } from '../../test/render';
import { CreateFromYaml } from './CreateFromYaml';

vi.mock('../../adapters/monaco/YamlEditor', () => ({
  YamlEditor: (p: { value: string; onChange: (v: string) => void; label: string }) => (
    <textarea aria-label={p.label} value={p.value} onChange={(e) => p.onChange(e.target.value)} />
  ),
}));

const two =
  'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n---\napiVersion: v1\nkind: Secret\nmetadata:\n  name: b\n';

function mount(answer: (body: { dryRun: boolean; documents: string[] }) => Response) {
  const bodies: { dryRun: boolean; documents: string[] }[] = [];
  const fetch = vi.fn((_url: string, init?: RequestInit) => {
    const body = JSON.parse(String(init?.body)) as { dryRun: boolean; documents: string[] };
    bodies.push(body);
    return Promise.resolve(answer(body));
  });
  renderApp(<CreateFromYaml cluster="v:prod" />, { client: createClient({ fetch }) });
  return { bodies, fetch };
}

async function enter(text: string) {
  const box = await screen.findByRole('textbox', { name: 'YAML to create' });
  await userEvent.clear(box);
  await userEvent.click(box);
  await userEvent.paste(text);
}

const ok = (n: number, action = 'created') => ({
  results: Array.from({ length: n }, (_, i) => ({
    index: i,
    kind: i === 0 ? 'ConfigMap' : 'Secret',
    namespace: 'default',
    name: i === 0 ? 'a' : 'b',
    action,
  })),
});

describe('CreateFromYaml', () => {
  it('sends a dry run first and, when every document passes, the real request; then lists the results', async () => {
    const { bodies, fetch } = mount(() => jsonResponse(ok(2)));
    await enter(two);
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    const list = await screen.findByRole('list', { name: 'Results' });
    expect(bodies.map((b) => b.dryRun)).toEqual([true, false]);
    expect(bodies[0]?.documents).toHaveLength(2);
    expect(fetch.mock.calls[0]?.[0]).toBe('/api/v1/clusters/v%3Aprod/apply');
    expect(list).toHaveTextContent('ConfigMap default/a: created');
    expect(list).toHaveTextContent('Secret default/b: created');
  });

  it('stops after a failing dry run and shows the error of that document', async () => {
    const { bodies } = mount(() =>
      jsonResponse({
        results: [
          { index: 0, kind: 'ConfigMap', namespace: 'default', name: 'a', action: 'created' },
          { index: 1, kind: 'Secret', namespace: 'default', name: 'b', error: 'webhook denied it' },
        ],
      }),
    );
    await enter(two);
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByText('Document 2 (Secret b): webhook denied it')).toBeInTheDocument();
    expect(screen.getByText('ConfigMap default/a: would be created')).toBeInTheDocument();
    expect(bodies).toHaveLength(1);
  });

  it('Preview only does the dry run', async () => {
    const { bodies } = mount(() => jsonResponse(ok(1, 'updated')));
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect(await screen.findByText('ConfigMap default/a: would be updated')).toBeInTheDocument();
    expect(bodies).toHaveLength(1);
    expect(bodies[0]?.dryRun).toBe(true);
  });

  it('shows a request error as text and an unnamed failure without kind', async () => {
    mount(() => jsonResponse({ error: { code: 'too_large', message: 'At most 100 documents' } }, 413));
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('At most 100 documents');
  });

  it('lists a document that is not valid YAML by its position', async () => {
    mount(() =>
      jsonResponse({
        results: [{ index: 0, kind: '', namespace: '', name: '', error: 'The document is not valid YAML' }],
      }),
    );
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect(await screen.findByText('Document 1: The document is not valid YAML')).toBeInTheDocument();
  });

  it('cannot be sent without a document', async () => {
    mount(() => jsonResponse(ok(0)));
    await enter('---\n');
    expect(screen.getByRole('button', { name: 'Create' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Preview' })).toBeDisabled();
  });

  it('reports a failure that has no API message in general words', async () => {
    const fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
    renderApp(<CreateFromYaml cluster="c" />, { client: createClient({ fetch }) });
    await userEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('The server could not be reached.');
  });
});
