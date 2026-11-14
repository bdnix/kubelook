import { act, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { LiveContext } from '../../state/useLive';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { makeLiveStore } from '../../test/live';
import { jsonResponse, renderApp } from '../../test/render';
import { PodFilesPage } from './PodFilesPage';

vi.mock('./FilesPanel', () => ({
  FilesPanel: ({ target }: { target: { namespace: string; pod: string; container: string } }) => (
    <p data-testid="panel">{`${target.namespace}/${target.pod}/${target.container}`}</p>
  ),
}));

const res = (group: string, resource: string, kind: string, namespaced: boolean) => ({
  group,
  version: 'v1',
  resource,
  kind,
  namespaced,
  verbs: ['list'],
  preferred: true,
  servedVersions: ['v1'],
});

const pod = (name: string, containers: [string, boolean][]) => ({
  metadata: { uid: name, name, namespace: 'shop' },
  status: {
    containerStatuses: containers.map(([n, up]) => ({
      name: n,
      state: up ? { running: {} } : { waiting: {} },
    })),
  },
});

describe('PodFilesPage', () => {
  it('picks a namespace, a pod and a running container, then shows the file browser for it', async () => {
    const store = makeLiveStore();
    const fetch = vi.fn((url: string) =>
      Promise.resolve(
        jsonResponse(
          url.endsWith('/discovery')
            ? {
                serverVersion: 'v1',
                resources: [res('', 'namespaces', 'Namespace', false), res('', 'pods', 'Pod', true)],
              }
            : {},
        ),
      ),
    );
    renderApp(
      <LiveContext.Provider value={store}>
        <PodFilesPage cluster="v:prod" />
      </LiveContext.Provider>,
      { client: createClient({ fetch }) },
    );
    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
    act(() => FakeWebSocket.last.serverOpen());
    const channelOf = (resource: string) =>
      FakeWebSocket.last.frames.find((f) => (f.d as { resource?: string } | undefined)?.resource === resource)
        ?.ch ?? 0;
    await waitFor(() => expect(channelOf('namespaces')).toBeGreaterThan(0));
    act(() =>
      FakeWebSocket.last.serverSend({
        ch: channelOf('namespaces'),
        t: 'snapshot',
        d: {
          items: [{ metadata: { uid: 'n2', name: 'shop' } }, { metadata: { uid: 'n1', name: 'blog' } }],
          more: false,
        },
      }),
    );
    const ns = await screen.findByLabelText('Namespace');
    expect(screen.getByLabelText('Pod')).toBeDisabled();
    await waitFor(() =>
      expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual([
        'Choose a namespace',
        'blog',
        'shop',
        'Choose a pod',
      ]),
    );
    await userEvent.selectOptions(ns, 'shop');
    await waitFor(() => expect(channelOf('pods')).toBeGreaterThan(0));
    act(() =>
      FakeWebSocket.last.serverSend({
        ch: channelOf('pods'),
        t: 'snapshot',
        d: {
          items: [
            pod('web-2', [
              ['app', true],
              ['crashed', false],
            ]),
            pod('web-1', [['app', true]]),
          ],
          more: false,
        },
      }),
    );
    await userEvent.selectOptions(await screen.findByLabelText('Pod'), 'web-2');
    expect(screen.getByTestId('panel')).toHaveTextContent('shop/web-2/app');
    expect(screen.getAllByRole('option', { name: /^(app|crashed)/ }).map((o) => o.textContent)).toEqual([
      'app',
    ]);
    await userEvent.selectOptions(screen.getByLabelText('Namespace'), '');
    expect(screen.queryByTestId('panel')).toBeNull();
  });
});
