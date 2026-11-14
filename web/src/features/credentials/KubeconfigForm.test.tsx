import 'fake-indexeddb/auto';
import { QueryClient } from '@tanstack/react-query';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { IDBFactory } from 'fake-indexeddb';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { KubeconfigForm } from './KubeconfigForm';
import { openVaultStore, type VaultStore } from './vaultStore';

let store: VaultStore;
beforeEach(() => {
  globalThis.indexedDB = new IDBFactory();
  store = openVaultStore('u1');
});

const ok = () =>
  jsonResponse({
    sealed: 'v1.k.sealed.copy',
    label: 'prod',
    clusters: [{ id: 'k:prod/ctx', name: 'c', context: 'ctx', user: 'u', server: 's' }],
  });

function spyClient(handler: () => Response) {
  const base = fakeClient({ 'POST /credentials/kubeconfig': handler });
  const bodies: unknown[] = [];
  return {
    client: { ...base, post: <T,>(p: string, b?: unknown) => (bodies.push(b), base.post<T>(p, b)) },
    bodies,
  };
}

describe('KubeconfigForm', () => {
  it('takes a pasted kubeconfig and a label, posts both and keeps the sealed copy', async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries');
    const { client, bodies } = spyClient(ok);
    renderApp(<KubeconfigForm />, { client, vaultStore: store, queryClient });

    const button = screen.getByRole('button', { name: 'Add kubeconfig' });
    expect(button).toBeDisabled();
    await user.type(screen.getByLabelText('Label'), 'prod');
    expect(button).toBeDisabled();
    await user.click(screen.getByLabelText('Kubeconfig'));
    await user.paste('apiVersion: v1');
    await user.click(button);

    await waitFor(async () => expect(await store.list()).toHaveLength(1));
    expect(bodies).toEqual([{ label: 'prod', content: 'apiVersion: v1' }]);
    expect((await store.list())[0]).toMatchObject({
      id: 'kubeconfig:prod',
      kind: 'kubeconfig',
      label: 'prod',
      sealed: 'v1.k.sealed.copy',
    });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['clusters'] });
    expect(screen.getByLabelText('Kubeconfig')).toHaveValue('');
    expect(screen.getByLabelText('Label')).toHaveValue('');
  });

  it('a chosen file fills the box and defaults the label to the file name without its extension', async () => {
    const user = userEvent.setup();
    renderApp(<KubeconfigForm />, { client: spyClient(ok).client, vaultStore: store });
    const file = new File(['apiVersion: v1\nkind: Config'], 'prod-cluster.yaml', { type: 'text/yaml' });
    await user.upload(screen.getByLabelText('Kubeconfig file'), file);
    await waitFor(() =>
      expect(screen.getByLabelText('Kubeconfig')).toHaveValue('apiVersion: v1\nkind: Config'),
    );
    expect(screen.getByLabelText('Label')).toHaveValue('prod-cluster');
  });

  it('keeps a label the user already typed when a file is chosen', async () => {
    const user = userEvent.setup();
    renderApp(<KubeconfigForm />, { client: spyClient(ok).client, vaultStore: store });
    await user.type(screen.getByLabelText('Label'), 'mine');
    await user.upload(screen.getByLabelText('Kubeconfig file'), new File(['x'], 'other.yml'));
    await waitFor(() => expect(screen.getByLabelText('Kubeconfig')).toHaveValue('x'));
    expect(screen.getByLabelText('Label')).toHaveValue('mine');
  });

  it('a file name without an extension is used as is', async () => {
    const user = userEvent.setup();
    renderApp(<KubeconfigForm />, { client: spyClient(ok).client, vaultStore: store });
    await user.upload(screen.getByLabelText('Kubeconfig file'), new File(['x'], 'config'));
    await waitFor(() => expect(screen.getByLabelText('Label')).toHaveValue('config'));
  });

  it("shows the server's rejection verbatim as text and keeps the input", async () => {
    const user = userEvent.setup();
    const message = 'kubeconfig user "eks-user": exec credential plugins are not supported <b>x</b>';
    const { client } = spyClient(() => jsonResponse({ error: { code: 'bad_request', message } }, 400));
    renderApp(<KubeconfigForm />, { client, vaultStore: store });
    await user.type(screen.getByLabelText('Label'), 'prod');
    await user.click(screen.getByLabelText('Kubeconfig'));
    await user.paste('x: y');
    await user.click(screen.getByRole('button', { name: 'Add kubeconfig' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(message);
    expect(screen.getByLabelText('Kubeconfig')).toHaveValue('x: y');
    expect(await store.list()).toEqual([]);
  });
});
