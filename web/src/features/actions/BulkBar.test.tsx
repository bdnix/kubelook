import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, renderApp } from '../../test/render';
import { BulkBar, bulkActionsFor, MAX_BULK } from './BulkBar';

const obj = (name: string, namespace = 'ns') => ({ metadata: { name, namespace, uid: `uid-${name}` } });

interface Sent {
  method: string;
  url: string;
  body?: unknown;
}

function mount(props: Partial<Parameters<typeof BulkBar>[0]> = {}, opts: { fail?: string; progress?: string } = {}) {
  const sent: Sent[] = [];
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET';
    const body: unknown = init?.body ? JSON.parse(String(init.body)) : undefined;
    if (method === 'GET' && url.includes('/jobs')) {
      return Promise.resolve(
        jsonResponse({
          jobs: opts.progress
            ? [
                {
                  id: 'job-9', cluster: 'v:prod', type: 'bulk', target: { name: 'delete' }, status: 'running',
                  progress: { done: 1, total: 2, text: opts.progress }, message: '', offers: [],
                  createdAt: '2026-11-14T12:00:00Z', updatedAt: '2026-11-14T12:00:00Z',
                },
              ]
            : [],
        }),
      );
    }
    sent.push({ method, url, body });
    if (opts.fail) return Promise.resolve(jsonResponse({ error: { code: 'bad_request', message: opts.fail } }, 400));
    return Promise.resolve(jsonResponse({ jobId: 'job-9' }));
  });
  const onDone = vi.fn();
  renderApp(
    <BulkBar
      cluster="v:prod"
      group=""
      version="v1"
      resource="pods"
      kind="Pod"
      objects={[obj('a'), obj('b', 'other')]}
      onDone={onDone}
      clipboard={{ writeText: vi.fn(() => Promise.resolve()) }}
      {...props}
    />,
    { client: createClient({ fetch }) },
  );
  return { sent, onDone };
}

describe('bulkActionsFor', () => {
  it('offers the actions that are valid for the kind', () => {
    expect(bulkActionsFor('', 'Node')).toEqual(['cordon', 'uncordon', 'drain', 'label', 'delete']);
    expect(bulkActionsFor('apps', 'Deployment')).toEqual(['restart', 'label', 'delete']);
    expect(bulkActionsFor('apps', 'DaemonSet')).toEqual(['restart', 'label', 'delete']);
    expect(bulkActionsFor('', 'Pod')).toEqual(['label', 'delete']);
    expect(bulkActionsFor('batch', 'CronJob')).toEqual(['label', 'delete']);
  });
});

describe('BulkBar', () => {
  it('shows nothing without a selection', () => {
    mount({ objects: [] });
    expect(screen.queryByRole('toolbar')).toBeNull();
  });

  it('counts the selection and offers the actions of the kind', () => {
    mount();
    const bar = screen.getByRole('toolbar', { name: 'Bulk actions' });
    expect(within(bar).getByText('2 selected')).toBeInTheDocument();
    expect(within(bar).getAllByRole('button').map((b) => b.textContent)).toEqual(['Label…', 'Delete']);
  });

  it('nodes get cordon, uncordon and drain', () => {
    mount({ group: '', resource: 'nodes', kind: 'Node', objects: [obj('n1', '')] });
    expect(screen.getAllByRole('button').map((b) => b.textContent)).toEqual([
      'Cordon',
      'Uncordon',
      'Drain…',
      'Label…',
      'Delete',
    ]);
  });

  it('confirming starts a bulk job with every target and shows its progress', async () => {
    const m = mount({}, { progress: 'Processed 1 of 2 items' });
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }));
    const dialog = screen.getByRole('dialog', { name: 'Delete 2 Pods' });
    expect(within(dialog).getAllByRole('listitem').map((i) => i.textContent)).toEqual(['ns/a', 'other/b']);
    await userEvent.click(within(dialog).getByRole('button', { name: 'Delete 2 objects' }));

    await waitFor(() => expect(m.sent).toHaveLength(1));
    expect(m.sent[0]).toEqual({
      method: 'POST',
      url: '/api/v1/clusters/v%3Aprod/bulk',
      body: {
        action: 'delete',
        targets: [
          { group: 'core', version: 'v1', resource: 'pods', namespace: 'ns', name: 'a' },
          { group: 'core', version: 'v1', resource: 'pods', namespace: 'other', name: 'b' },
        ],
      },
    });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(await screen.findByText('Processed 1 of 2 items')).toBeInTheDocument();
    expect(m.onDone).toHaveBeenCalled();
  });

  it('sends the label parameters', async () => {
    const m = mount();
    await userEvent.click(screen.getByRole('button', { name: 'Label…' }));
    await userEvent.type(screen.getByLabelText('Labels to add'), 'env=prod');
    await userEvent.click(screen.getByRole('button', { name: 'Label 2 objects' }));
    await waitFor(() => expect(m.sent).toHaveLength(1));
    expect((m.sent[0]?.body as { params: unknown }).params).toEqual({ add: { env: 'prod' }, remove: [] });
  });

  it('shows the server error and keeps the dialog open', async () => {
    mount({}, { fail: 'Select fewer objects.' });
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await userEvent.click(screen.getByRole('button', { name: 'Delete 2 objects' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Select fewer objects.');
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('cancel closes the dialog without a request', async () => {
    const m = mount();
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(m.sent).toEqual([]);
  });

  it('refuses more than 500 objects', () => {
    mount({ objects: Array.from({ length: MAX_BULK + 1 }, (_, i) => obj(`p${i}`)) });
    expect(screen.getByText('Select at most 500 objects.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Delete' })).toBeDisabled();
  });
});
