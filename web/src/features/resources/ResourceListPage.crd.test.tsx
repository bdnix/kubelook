import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { LiveContext } from '../../state/useLive';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { makeLiveStore } from '../../test/live';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { ResourceListPage } from './ResourceListPage';

vi.mock('../../adapters/monaco/YamlEditor', () => ({
  YamlEditor: (p: { value: string; onChange: (v: string) => void; label: string }) => (
    <textarea aria-label={p.label} value={p.value} onChange={(e) => p.onChange(e.target.value)} />
  ),
}));

const discovery = {
  serverVersion: 'v1.35.0',
  resources: [
    {
      group: 'example.com',
      version: 'v1',
      resource: 'widgets',
      kind: 'Widget',
      namespaced: true,
      verbs: ['list'],
      preferred: true,
      servedVersions: ['v1'],
    },
  ],
};

const crd = {
  spec: {
    group: 'example.com',
    names: { kind: 'Widget', plural: 'widgets' },
    scope: 'Namespaced',
    versions: [
      {
        name: 'v1',
        additionalPrinterColumns: [{ name: 'State', type: 'string', jsonPath: '.status.state' }],
      },
    ],
  },
};

const widget = (name: string) => ({
  metadata: { uid: name, name, namespace: 'ns', creationTimestamp: '2026-11-14T10:00:00Z' },
  status: { state: 'Ready' },
});

function mount(crdAnswer: () => Response) {
  const store = makeLiveStore();
  const client = fakeClient({
    'GET /clusters': () =>
      jsonResponse({
        sources: [
          {
            type: 'vault',
            label: 'Vault',
            status: 'ok',
            clusters: [{ id: 'v:prod', name: 'prod', state: 'open' }],
          },
        ],
      }),
    'POST /clusters/v%3Aprod/open': () => jsonResponse({ state: 'open' }),
    'GET /clusters/v%3Aprod/discovery': () => jsonResponse(discovery),
    'GET /clusters/v%3Aprod/resources/apiextensions.k8s.io/v1/customresourcedefinitions/widgets.example.com':
      crdAnswer,
  });
  renderApp(
    <LiveContext.Provider value={store}>
      <ResourceListPage cluster="v:prod" group="example.com" kind="Widget" />
    </LiveContext.Provider>,
    { client },
  );
}

async function withWidgets() {
  await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
  act(() => FakeWebSocket.last.serverOpen());
  act(() =>
    FakeWebSocket.last.serverSend({ ch: 1, t: 'snapshot', d: { items: [widget('w1')], more: false } }),
  );
  return screen.findByRole('grid', { name: 'Widget' });
}

describe('ResourceListPage for a custom kind', () => {
  it('shows the printer columns of the CRD', async () => {
    mount(() => jsonResponse(crd));
    const grid = await withWidgets();
    await waitFor(() => expect(within(grid).getByRole('columnheader', { name: /State/ })).toBeInTheDocument());
    expect(within(grid).getByText('Ready')).toBeInTheDocument();
    expect(within(grid).getAllByRole('columnheader').map((h) => h.textContent?.replace(/\W+$/, ''))).toEqual([
      expect.stringContaining('Name'),
      expect.stringContaining('Namespace'),
      expect.stringContaining('State'),
      expect.stringContaining('Age'),
    ]);
  });

  it('falls back to Name, Namespace and Age when the CRD cannot be read', async () => {
    mount(() => jsonResponse({ error: { code: 'forbidden', message: 'no' } }, 403));
    const grid = await withWidgets();
    expect(within(grid).getByText('w1')).toBeInTheDocument();
    expect(within(grid).queryByRole('columnheader', { name: /State/ })).toBeNull();
  });

  it('opens Create from YAML with a template of the kind', async () => {
    mount(() => jsonResponse(crd));
    await withWidgets();
    await userEvent.click(screen.getByRole('button', { name: 'Create from YAML' }));
    expect(await screen.findByRole('textbox', { name: 'YAML to create' })).toHaveValue(
      'apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: example\n  namespace: default\n',
    );
  });
});
