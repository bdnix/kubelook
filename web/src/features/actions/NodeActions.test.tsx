import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, renderApp } from '../../test/render';
import { ActionMenu } from './ActionMenu';

const node = {
  metadata: { name: 'node-1', uid: 'n1' },
  spec: { taints: [{ key: 'dedicated', value: 'gpu', effect: 'NoSchedule' }] },
};

interface Sent {
  method: string;
  url: string;
  body?: unknown;
}

function mount() {
  const sent: Sent[] = [];
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    const body: unknown = init?.body ? JSON.parse(String(init.body)) : undefined;
    if (url.endsWith('/access')) {
      const checks = (body as { checks: unknown[] }).checks;
      return Promise.resolve(jsonResponse({ results: checks.map(() => ({ allowed: true })) }));
    }
    if ((init?.method ?? 'GET') === 'GET') return Promise.resolve(jsonResponse({ jobs: [] }));
    sent.push({ method: init?.method ?? 'GET', url, body });
    return Promise.resolve(jsonResponse(url.endsWith('/drain') ? { jobId: 'j1' } : { ok: true }));
  });
  renderApp(
    <ActionMenu
      cluster="v:prod"
      object={node}
      group=""
      version="v1"
      resource="nodes"
      kind="Node"
      actions={['cordon', 'uncordon', 'taint', 'drain']}
      clipboard={{ writeText: vi.fn(() => Promise.resolve()) }}
    />,
    { client: createClient({ fetch }) },
  );
  return { sent };
}

async function choose(name: string) {
  await userEvent.click(screen.getByRole('button', { name: 'Actions' }));
  await userEvent.click(within(screen.getByRole('menu', { name: 'Actions' })).getByRole('menuitem', { name }));
}

const target = { group: 'core', version: 'v1', resource: 'nodes', namespace: '', name: 'node-1' };

describe('node actions', () => {
  it('lists cordon, uncordon, taints and drain', async () => {
    mount();
    await userEvent.click(screen.getByRole('button', { name: 'Actions' }));
    expect(screen.getAllByRole('menuitem').map((i) => i.textContent)).toEqual([
      'Cordon',
      'Uncordon',
      'Taints…',
      'Drain…',
    ]);
  });

  it('cordon and uncordon run at once', async () => {
    const { sent } = mount();
    await choose('Cordon');
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toEqual({
      method: 'POST',
      url: '/api/v1/clusters/v%3Aprod/actions',
      body: { action: 'cordon', target },
    });
    await choose('Uncordon');
    await waitFor(() => expect(sent).toHaveLength(2));
    expect((sent[1]?.body as { action: string }).action).toBe('uncordon');
  });

  it('taints: lists them, adds one and removes one', async () => {
    const { sent } = mount();
    await choose('Taints…');
    const dialog = screen.getByRole('dialog', { name: 'Taints of node-1' });
    expect(within(dialog).getByText('dedicated=gpu:NoSchedule')).toBeInTheDocument();
    await userEvent.type(within(dialog).getByLabelText('Key'), 'team');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add taint' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]?.body).toEqual({
      action: 'taintAdd',
      target,
      params: { key: 'team', effect: 'NoSchedule' },
    });

    await choose('Taints…');
    await userEvent.click(screen.getByRole('button', { name: 'Remove dedicated=gpu:NoSchedule' }));
    await waitFor(() => expect(sent).toHaveLength(2));
    expect(sent[1]?.body).toEqual({
      action: 'taintRemove',
      target,
      params: { key: 'dedicated', effect: 'NoSchedule' },
    });
  });

  it('drain asks first, names the emptyDir consequence and starts a job', async () => {
    const { sent } = mount();
    await choose('Drain…');
    const dialog = screen.getByRole('dialog', { name: 'Drain Node node-1?' });
    expect(within(dialog).getByText(/emptyDir volumes are evicted too/)).toBeInTheDocument();
    expect(sent).toEqual([]);
    await userEvent.click(within(dialog).getByRole('button', { name: 'Drain' }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toEqual({ method: 'POST', url: '/api/v1/clusters/v%3Aprod/nodes/node-1/drain', body: undefined });
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });
});
