import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import type { K8sObject } from '../../state/live';
import { jsonResponse, renderApp } from '../../test/render';
import { EditorTab } from './EditorTab';
import type { EditTarget } from './useEditorState';

vi.mock('../../adapters/monaco/YamlEditor', () => ({
  YamlEditor: (p: {
    value: string;
    onChange: (v: string) => void;
    readOnly?: boolean;
    label: string;
    schemaRef?: string;
  }) => (
    <textarea
      aria-label={p.label}
      value={p.value}
      readOnly={p.readOnly}
      data-schema-ref={p.schemaRef ?? ''}
      onChange={(e) => p.onChange(e.target.value)}
    />
  ),
}));
vi.mock('../../adapters/monaco/DiffViewer', () => ({
  DiffViewer: (p: { original: string; modified: string; label: string }) => (
    <div role="region" aria-label={p.label}>
      <pre data-testid="original">{p.original}</pre>
      <pre data-testid="modified">{p.modified}</pre>
    </div>
  ),
}));

const target: EditTarget = {
  cluster: 'c',
  group: '',
  version: 'v1',
  resource: 'configmaps',
  namespace: 'ns',
  name: 'cm',
};
const cm = (rv: string, a = '1'): K8sObject => ({
  apiVersion: 'v1',
  kind: 'ConfigMap',
  metadata: { name: 'cm', namespace: 'ns', resourceVersion: rv },
  data: { a },
});

interface Call {
  method: string;
  url: string;
}

function mount(opts: { canEdit?: boolean; object?: K8sObject; respond?: (c: Call) => Response } = {}) {
  const calls: Call[] = [];
  const respond =
    opts.respond ??
    ((c: Call) =>
      c.url.includes('/openapi/')
        ? jsonResponse({
            components: {
              schemas: {
                CM: { 'x-kubernetes-group-version-kind': [{ group: '', version: 'v1', kind: 'ConfigMap' }] },
              },
            },
          })
        : jsonResponse(cm('9', '2')));
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    const call = { method: init?.method ?? 'GET', url };
    calls.push(call);
    return Promise.resolve(respond(call));
  });
  const view = renderApp(
    <EditorTab
      target={target}
      kind="ConfigMap"
      object={opts.object ?? cm('7')}
      canEdit={opts.canEdit ?? true}
    />,
    {
      client: createClient({ fetch }),
    },
  );
  return { calls, ...view };
}

const ready = () => screen.findByRole('textbox', { name: 'ConfigMap YAML' });
const editorText = () => (editor() as HTMLTextAreaElement).value;
const editor = () => screen.getByRole('textbox', { name: 'ConfigMap YAML' });
async function type(text: string) {
  await userEvent.clear(editor());
  await userEvent.click(editor());
  await userEvent.paste(text);
}

describe('EditorTab', () => {
  it('shows the object as YAML, passes the schema on, and the kubectl equivalent', async () => {
    mount();
    await ready();
    expect(editorText()).toContain('kind: ConfigMap');
    expect(screen.getByText('kubectl replace -f - -n ns')).toBeInTheDocument();
    await vi.waitFor(() => expect(editor()).toHaveAttribute('data-schema-ref', '#/components/schemas/CM'));
  });

  it('keeps Preview and Save off until the text is changed and valid', async () => {
    mount();
    expect(screen.getByRole('button', { name: 'Preview' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Discard changes' })).toBeDisabled();
    await userEvent.type(editor(), '# note');
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled();
  });

  it('Preview shows the diff of live against result', async () => {
    mount({
      respond: (c) =>
        c.url.includes('/openapi/')
          ? jsonResponse({})
          : jsonResponse({ live: cm('7', '1'), result: cm('7', '2') }),
    });
    await userEvent.type(editor(), '# x');
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }));
    const diff = await screen.findByLabelText('Preview of the change');
    expect(diff).toHaveTextContent('- ');
    expect(diff).toHaveTextContent('a: "2"');
  });

  it('Save sends the PUT and says it was saved', async () => {
    const { calls } = mount();
    await userEvent.type(editor(), '# x');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Saved.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'PUT' && !c.url.includes('dryRun'))).toBe(true);
  });

  it('shows an admission error as text', async () => {
    mount({
      respond: (c) =>
        c.url.includes('/openapi/')
          ? jsonResponse({})
          : jsonResponse({ error: { code: 'unprocessable', message: 'denied: no latest tags' } }, 422),
    });
    await userEvent.type(editor(), '# x');
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('denied: no latest tags');
  });

  it('without a dry run it explains and offers Save without preview', async () => {
    mount({
      respond: (c) =>
        c.url.includes('/openapi/')
          ? jsonResponse({})
          : c.url.includes('dryRun')
            ? jsonResponse({ error: { code: 'dry_run_unsupported', message: 'x' } }, 422)
            : jsonResponse(cm('9')),
    });
    await userEvent.type(editor(), '# x');
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect(await screen.findByText("A preview isn't available for this object.")).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Preview' })).toBeDisabled();
    await userEvent.click(screen.getByRole('button', { name: 'Save without preview' }));
    expect(await screen.findByText('Saved.')).toBeInTheDocument();
  });

  it('a conflict on save shows the banner; Reload takes the live version', async () => {
    mount({
      respond: (c) =>
        c.url.includes('/openapi/')
          ? jsonResponse({})
          : jsonResponse(
              { error: { code: 'conflict', message: 'The object changed since you loaded it.' } },
              409,
            ),
    });
    await userEvent.type(editor(), '# mine');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText(/your changes were not saved/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Reload' }));
    expect(editorText()).not.toContain('# mine');
    expect(screen.queryByText(/your changes were not saved/)).toBeNull();
  });

  it('offers a newer live version while dirty, with a diff against mine', async () => {
    const { rerender } = mount();
    await ready();
    await userEvent.type(editor(), '# mine');
    rerender(<EditorTab target={target} kind="ConfigMap" object={cm('8', 'theirs')} canEdit />);
    expect(await screen.findByText(/changed in the cluster while you were editing/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Diff against mine' }));
    const diff = await screen.findByRole('region', { name: 'Live ConfigMap against your changes' });
    expect(diff).toHaveTextContent('a: theirs');
    expect(diff).toHaveTextContent('# mine');
    await userEvent.click(screen.getByRole('button', { name: 'Back to the editor' }));
    expect(editor()).toBeInTheDocument();
  });

  it('is read only without permission to update', () => {
    const { calls } = mount({ canEdit: false });
    expect(editor()).toHaveAttribute('readonly');
    expect(screen.getByText('You can read this object but not change it.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
    expect(calls.some((c) => c.url.includes('/openapi/'))).toBe(false);
  });

  it('blocks Save for invalid YAML and for a changed name', async () => {
    mount();
    await type('a: [');
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
    expect(screen.getAllByRole('alert').length).toBeGreaterThan(0);
    await type(
      'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: other\n  namespace: ns\n  resourceVersion: "7"\n',
    );
    expect(screen.getByText(/can't be changed/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  });
});

describe('EditorTab: Secrets', () => {
  const secret: K8sObject = {
    apiVersion: 'v1',
    kind: 'Secret',
    metadata: { name: 'db', namespace: 'ns', resourceVersion: '3' },
    data: { password: '********', token: '********' },
  };
  const secretTarget: EditTarget = { ...target, resource: 'secrets', name: 'db' };

  it('keeps masks as they are and reveals one key into the text for editing', async () => {
    const urls: string[] = [];
    const fetch = vi.fn((url: string) => {
      urls.push(url);
      return Promise.resolve(
        url.includes('/reveal') ? jsonResponse({ data: { password: 'c2VjcmV0' } }) : jsonResponse({}),
      );
    });
    renderApp(<EditorTab target={secretTarget} kind="Secret" object={secret} canEdit />, {
      client: createClient({ fetch }),
    });
    const box = (await screen.findByRole('textbox', { name: 'Secret YAML' })) as HTMLTextAreaElement;
    expect(box.value).toContain('password: "********"');
    await userEvent.click(screen.getByRole('button', { name: 'Reveal password for editing' }));
    await vi.waitFor(() => expect(box.value).toContain('password: c2VjcmV0'));
    expect(box.value).toContain('token: "********"');
    expect(screen.queryByRole('button', { name: 'Reveal password for editing' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Reveal token for editing' })).toBeInTheDocument();
    expect(urls.some((u) => u.endsWith('/secrets/reveal'))).toBe(true);
  });

  it('read-only editors and other kinds offer no reveal', async () => {
    renderApp(<EditorTab target={secretTarget} kind="Secret" object={secret} canEdit={false} />, {
      client: createClient({ fetch: () => Promise.resolve(jsonResponse({})) }),
    });
    await screen.findByRole('textbox', { name: 'Secret YAML' });
    expect(screen.queryByRole('region', { name: 'Secret values' })).toBeNull();
  });

  it('shows a refused reveal as text and leaves the mask', async () => {
    const fetch = vi.fn((url: string) =>
      Promise.resolve(
        url.includes('/reveal')
          ? jsonResponse({ error: { code: 'forbidden', message: 'no access' } }, 403)
          : jsonResponse({}),
      ),
    );
    renderApp(<EditorTab target={secretTarget} kind="Secret" object={secret} canEdit />, {
      client: createClient({ fetch }),
    });
    await userEvent.click(await screen.findByRole('button', { name: 'Reveal token for editing' }));
    expect(await screen.findByText('no access')).toBeInTheDocument();
    expect((screen.getByRole('textbox', { name: 'Secret YAML' }) as HTMLTextAreaElement).value).toContain(
      'token: "********"',
    );
  });
});
