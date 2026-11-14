import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, renderApp } from '../../test/render';
import { ActionMenu, type ActionMenuProps } from './ActionMenu';

const deployment = {
  metadata: { name: 'web', namespace: 'ns', annotations: { 'deployment.kubernetes.io/revision': '3' } },
  spec: {
    replicas: 2,
    template: {
      spec: {
        containers: [{ name: 'app', image: 'nginx:1' }],
        initContainers: [{ name: 'setup', image: 'busybox' }],
      },
    },
  },
};

interface Sent {
  method: string;
  url: string;
  body?: unknown;
}

function mount(
  props: Partial<ActionMenuProps> = {},
  opts: { allowed?: (check: { verb: string; resource: string }) => boolean; fail?: string } = {},
) {
  const sent: Sent[] = [];
  const clipboard = { writeText: vi.fn(() => Promise.resolve()) };
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    const body: unknown = init?.body ? JSON.parse(String(init.body)) : undefined;
    if (url.endsWith('/access')) {
      const checks = (body as { checks: { verb: string; resource: string }[] }).checks;
      return Promise.resolve(
        jsonResponse({ results: checks.map((c) => ({ allowed: opts.allowed?.(c) ?? true })) }),
      );
    }
    sent.push({ method: init?.method ?? 'GET', url, body });
    if (opts.fail)
      return Promise.resolve(jsonResponse({ error: { code: 'conflict', message: opts.fail } }, 409));
    return Promise.resolve(jsonResponse({ ok: true }));
  });
  renderApp(
    <ActionMenu
      cluster="v:prod"
      object={deployment}
      group="apps"
      version="v1"
      resource="deployments"
      kind="Deployment"
      actions={['scale', 'restart', 'pause', 'resume', 'rollback', 'setImage', 'delete', 'cordon']}
      clipboard={clipboard}
      {...props}
    />,
    { client: createClient({ fetch }) },
  );
  return { sent, clipboard };
}

async function openMenu() {
  await userEvent.click(screen.getByRole('button', { name: 'Actions' }));
  return within(screen.getByRole('menu', { name: 'Actions' }));
}

describe('ActionMenu', () => {
  it('lists the actions of the kind and ignores ids it does not know', async () => {
    mount();
    const menu = await openMenu();
    expect(menu.getAllByRole('menuitem').map((i) => i.textContent)).toEqual([
      'Scale…',
      'Rollout restart',
      'Pause rollout',
      'Resume rollout',
      'Roll back…',
      'Set image…',
      'Delete',
    ]);
  });

  it('renders nothing for a kind without known actions', () => {
    mount({ actions: ['cordon'] });
    expect(screen.queryByRole('button', { name: 'Actions' })).toBeNull();
  });

  it('disables an action the user may not do and says which permission is missing', async () => {
    mount({}, { allowed: (c) => c.verb !== 'delete' });
    const menu = await openMenu();
    const del = await vi.waitFor(() => {
      const item = menu.getByRole('menuitem', { name: 'Delete' });
      expect(item).toBeDisabled();
      return item;
    });
    expect(del).toHaveAttribute('title', 'missing permission: delete deployments');
    expect(menu.getByRole('menuitem', { name: 'Scale…' })).toBeEnabled();
  });

  it('asks the access endpoint for the permission of every action, scale on the subresource', async () => {
    const sentChecks: unknown[] = [];
    const fetch = vi.fn((url: string, init?: RequestInit) => {
      if (url.endsWith('/access'))
        sentChecks.push(...(JSON.parse(String(init?.body)) as { checks: unknown[] }).checks);
      return Promise.resolve(jsonResponse({ results: [] }));
    });
    renderApp(
      <ActionMenu
        cluster="c"
        object={deployment}
        group="apps"
        version="v1"
        resource="deployments"
        kind="Deployment"
        actions={['scale', 'delete']}
      />,
      { client: createClient({ fetch }) },
    );
    await vi.waitFor(() => expect(sentChecks).toHaveLength(2));
    expect(sentChecks[0]).toMatchObject({
      verb: 'patch',
      resource: 'deployments',
      subresource: 'scale',
      namespace: 'ns',
      name: 'web',
    });
    expect(sentChecks[1]).toMatchObject({ verb: 'delete', resource: 'deployments' });
  });

  it('is disabled for an object that is gone', () => {
    mount({ disabled: true });
    expect(screen.getByRole('button', { name: 'Actions' })).toBeDisabled();
  });

  it('runs a harmless action at once', async () => {
    const { sent } = mount();
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Pause rollout' }));
    await vi.waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toEqual({
      method: 'POST',
      url: '/api/v1/clusters/v%3Aprod/actions',
      body: {
        action: 'pause',
        target: { group: 'apps', version: 'v1', resource: 'deployments', namespace: 'ns', name: 'web' },
      },
    });
  });

  it('scale validates the number and sends the action', async () => {
    const { sent } = mount();
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Scale…' }));
    const dialog = screen.getByRole('dialog', { name: 'Scale Deployment web' });
    const input = within(dialog).getByLabelText('Replicas');
    expect(input).toHaveValue(2);
    await userEvent.clear(input);
    expect(within(dialog).getByRole('button', { name: 'Scale' })).toBeDisabled();
    await userEvent.type(input, '5');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Scale' }));
    await vi.waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]?.body).toMatchObject({ action: 'scale', params: { replicas: 5 } });
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('every dialog offers the kubectl command to copy', async () => {
    const { clipboard } = mount();
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Scale…' }));
    const dialog = screen.getByRole('dialog');
    expect(within(dialog).getByText('kubectl scale deployment/web --replicas=2 -n ns')).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Copy as kubectl' }));
    expect(clipboard.writeText).toHaveBeenCalledWith('kubectl scale deployment/web --replicas=2 -n ns');
  });

  it('destructive actions name the object and wait for confirmation; Cancel and Escape close', async () => {
    const { sent } = mount();
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Delete' }));
    const dialog = screen.getByRole('dialog', { name: 'Delete Deployment web?' });
    expect(dialog).toHaveTextContent('Deployment web in ns');
    expect(dialog).toHaveFocus();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Rollout restart' }));
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(sent).toHaveLength(0);
  });

  it('delete sends DELETE on the object', async () => {
    const { sent } = mount();
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Delete' }));
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await vi.waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toMatchObject({
      method: 'DELETE',
      url: '/api/v1/clusters/v%3Aprod/resources/apps/v1/deployments/web?namespace=ns',
    });
  });

  it('delete of a cluster-scoped object has no namespace in the URL', async () => {
    const { sent } = mount({
      object: { metadata: { name: 'n1' } },
      group: '',
      resource: 'nodes',
      kind: 'Node',
      actions: ['delete'],
    });
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Delete' }));
    expect(screen.getByRole('dialog')).toHaveTextContent('This acts on Node n1.');
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await vi.waitFor(() => expect(sent[0]?.url).toBe('/api/v1/clusters/v%3Aprod/resources/core/v1/nodes/n1'));
  });

  it('restart and evict ask first and send their action', async () => {
    const { sent } = mount();
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Rollout restart' }));
    expect(screen.getByRole('dialog', { name: 'Restart Deployment web?' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Restart' }));
    await vi.waitFor(() => expect(sent[0]?.body).toMatchObject({ action: 'restart' }));
  });

  it('evict shows the eviction command', async () => {
    mount({
      object: { metadata: { name: 'web-1', namespace: 'ns' } },
      group: '',
      resource: 'pods',
      kind: 'Pod',
      actions: ['evict'],
    });
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Evict' }));
    expect(screen.getByRole('dialog', { name: 'Evict Pod web-1?' })).toHaveTextContent(
      '/api/v1/namespaces/ns/pods/web-1/eviction',
    );
  });

  it('shows the server message when the action fails and keeps the dialog', async () => {
    mount({}, { fail: 'Cannot evict: a PodDisruptionBudget would be violated.' });
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Delete' }));
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Cannot evict: a PodDisruptionBudget would be violated.',
    );
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('set image offers containers and init containers and rejects whitespace', async () => {
    const { sent } = mount();
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Set image…' }));
    const dialog = screen.getByRole('dialog', { name: 'Set image of Deployment web' });
    expect(
      within(dialog)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['app', 'setup (init)']);
    const image = within(dialog).getByLabelText('Image');
    expect(image).toHaveValue('nginx:1');
    await userEvent.selectOptions(within(dialog).getByLabelText('Container'), '1');
    expect(image).toHaveValue('busybox');
    await userEvent.clear(image);
    await userEvent.type(image, 'busy box');
    expect(within(dialog).getByRole('button', { name: 'Set image' })).toBeDisabled();
    await userEvent.clear(image);
    await userEvent.type(image, 'busybox:2');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Set image' }));
    await vi.waitFor(() =>
      expect(sent[0]?.body).toMatchObject({
        action: 'setImage',
        params: { container: 'setup', image: 'busybox:2', init: true },
      }),
    );
  });

  it('rollback lists the earlier revisions, or says there are none', async () => {
    const { sent } = mount({
      revisions: [
        { revision: 2, images: ['nginx:2'] },
        { revision: 1, images: ['nginx:1', 'side:1'] },
      ],
    });
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Roll back…' }));
    const dialog = screen.getByRole('dialog', { name: 'Roll back Deployment web' });
    expect(
      within(dialog)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['2: nginx:2', '1: nginx:1, side:1']);
    await userEvent.selectOptions(within(dialog).getByLabelText('Revision'), '1');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Roll back' }));
    await vi.waitFor(() =>
      expect(sent[0]?.body).toMatchObject({ action: 'rollback', params: { revision: 1 } }),
    );
  });

  it('rollback without revisions cannot be confirmed', async () => {
    mount();
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Roll back…' }));
    expect(screen.getByText('There is no earlier revision to roll back to.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Roll back' })).toBeDisabled();
  });

  it('expands a volume claim only to a different, well formed size', async () => {
    const { sent } = mount({
      object: {
        metadata: { name: 'data', namespace: 'ns' },
        spec: { resources: { requests: { storage: '10Gi' } } },
      },
      group: '',
      resource: 'persistentvolumeclaims',
      kind: 'PersistentVolumeClaim',
      actions: ['expandPVC'],
    });
    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Expand…' }));
    const dialog = screen.getByRole('dialog');
    const size = within(dialog).getByLabelText('New size');
    expect(within(dialog).getByRole('button', { name: 'Expand' })).toBeDisabled();
    await userEvent.clear(size);
    await userEvent.type(size, 'lots');
    expect(within(dialog).getByRole('button', { name: 'Expand' })).toBeDisabled();
    await userEvent.clear(size);
    await userEvent.type(size, '20Gi');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Expand' }));
    await vi.waitFor(() =>
      expect(sent[0]?.body).toMatchObject({ action: 'expandPVC', params: { storage: '20Gi' } }),
    );
  });

  it('a network failure is reported in general words', async () => {
    const fetch = vi.fn((url: string) =>
      url.endsWith('/access')
        ? Promise.resolve(jsonResponse({ results: [{ allowed: true }] }))
        : Promise.reject(new TypeError('offline')),
    );
    renderApp(
      <ActionMenu
        cluster="c"
        object={deployment}
        group="apps"
        version="v1"
        resource="deployments"
        kind="Deployment"
        actions={['delete']}
      />,
      { client: createClient({ fetch }) },
    );
    await userEvent.click(await screen.findByRole('button', { name: 'Actions' }));
    await userEvent.click(screen.getByRole('menuitem', { name: 'Delete' }));
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('The server could not be reached.');
  });
});
