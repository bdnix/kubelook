import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes, useLocation } from 'react-router';
import { describe, expect, it } from 'vitest';
import { FakeWebSocket } from '../../test/FakeWebSocket';
import { makeSocket } from '../../test/socket';
import { SocketContext } from '../../ws/context';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { ResourceMapPage } from './ResourceMapPage';
import { RelatedMap } from './RelatedMap';

const Where = () => <p data-testid="where">{useLocation().pathname}</p>;
const settings = (namespaces: string[]) => ({
  namespaceFilters: { c: namespaces },
  favouriteClusters: [],
  filterSets: [],
  columnPresets: {},
  theme: 'system',
  watchIdleSeconds: 30,
});

const graph = {
  nodes: [
    { id: 'Deployment/shop/web', group: 'apps', kind: 'Deployment', namespace: 'shop', name: 'web' },
    { id: 'Pod/shop/web-1', group: '', kind: 'Pod', namespace: 'shop', name: 'web-1', status: 'Failed' },
    { id: 'Service/shop/web', group: '', kind: 'Service', namespace: 'shop', name: 'web' },
  ],
  edges: [
    { from: 'Deployment/shop/web', to: 'Pod/shop/web-1', type: 'owns' },
    { from: 'Service/shop/web', to: 'Pod/shop/web-1', type: 'selects' },
  ],
  omittedKinds: ['Secret'],
  leftOutNamespaces: ['big'],
};

async function openAndSend(g: unknown) {
  await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
  act(() => FakeWebSocket.last.serverOpen());
  await waitFor(() => expect(FakeWebSocket.last.frames.some((f) => f.op === 'map.open')).toBe(true));
  act(() => FakeWebSocket.last.serverSend({ ch: 1, t: 'graph', d: g }));
}

describe('ResourceMapPage', () => {
  it('draws the nodes and links of a graph frame, with the legend', async () => {
    renderApp(
      <SocketContext.Provider value={makeSocket()}>
        <Routes>
          <Route path="/c/:cluster/resource-map" element={<ResourceMapPage cluster="c" />} />
          <Route path="*" element={<Where />} />
        </Routes>
      </SocketContext.Provider>,
      {
        client: fakeClient({ 'GET /settings': () => jsonResponse(settings(['shop'])) }),
        route: '/c/c/resource-map',
      },
    );
    await openAndSend(graph);
    expect(await screen.findByRole('button', { name: 'Pod web-1, failed' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Deployment web' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Service web' })).toBeInTheDocument();
    expect(document.querySelectorAll('line')).toHaveLength(2);
    expect(screen.getByRole('region', { name: 'Legend' })).toBeInTheDocument();
    expect(FakeWebSocket.last.frames.find((f) => f.op === 'map.open')?.d).toEqual({
      cluster: 'c',
      namespaces: ['shop'],
    });
  });

  it('names the kinds left out and the namespaces that did not fit', async () => {
    renderApp(
      <SocketContext.Provider value={makeSocket()}>
        <ResourceMapPage cluster="c" />
      </SocketContext.Provider>,
      { client: fakeClient({ 'GET /settings': () => jsonResponse(settings([])) }) },
    );
    await openAndSend(graph);
    expect(await screen.findByText(/You may not list Secret/)).toBeInTheDocument();
    expect(screen.getByText(/big/)).toHaveTextContent(/could not be shown/);
  });

  it('opens the details of a node on click', async () => {
    renderApp(
      <SocketContext.Provider value={makeSocket()}>
        <Routes>
          <Route path="/c/:cluster/resource-map" element={<ResourceMapPage cluster="c" />} />
          <Route path="*" element={<Where />} />
        </Routes>
      </SocketContext.Provider>,
      {
        client: fakeClient({ 'GET /settings': () => jsonResponse(settings([])) }),
        route: '/c/c/resource-map',
      },
    );
    await openAndSend(graph);
    await userEvent.click(await screen.findByRole('button', { name: 'Deployment web' }));
    expect(screen.getByTestId('where')).toHaveTextContent('/c/c/ns/shop/apps/Deployment/web');
  });

  it('says so when the map is empty', async () => {
    renderApp(
      <SocketContext.Provider value={makeSocket()}>
        <ResourceMapPage cluster="c" />
      </SocketContext.Provider>,
      { client: fakeClient({ 'GET /settings': () => jsonResponse(settings([])) }) },
    );
    await openAndSend({ nodes: [], edges: [], omittedKinds: [] });
    expect(await screen.findByText('Nothing to show.')).toBeInTheDocument();
  });

  it('shows the error of the channel', async () => {
    renderApp(
      <SocketContext.Provider value={makeSocket()}>
        <ResourceMapPage cluster="c" />
      </SocketContext.Provider>,
      { client: fakeClient({ 'GET /settings': () => jsonResponse(settings([])) }) },
    );
    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0));
    act(() => FakeWebSocket.last.serverOpen());
    await waitFor(() => expect(FakeWebSocket.last.frames.some((f) => f.op === 'map.open')).toBe(true));
    act(() =>
      FakeWebSocket.last.serverSend({ ch: 1, t: 'error', d: { code: 'forbidden', message: 'No access.' } }),
    );
    expect(await screen.findByText('No access.')).toBeInTheDocument();
  });
});

describe('RelatedMap', () => {
  it('opens the map focused on the object', async () => {
    renderApp(
      <SocketContext.Provider value={makeSocket()}>
        <RelatedMap cluster="c" group="apps" kind="Deployment" namespace="shop" name="web" />
      </SocketContext.Provider>,
    );
    await openAndSend(graph);
    expect(FakeWebSocket.last.frames.find((f) => f.op === 'map.open')?.d).toEqual({
      cluster: 'c',
      namespaces: ['shop'],
      focus: { group: 'apps', kind: 'Deployment', namespace: 'shop', name: 'web' },
    });
    const map = await screen.findByRole('region', { name: 'Related resources' });
    expect(within(map).getByRole('button', { name: 'Pod web-1, failed' })).toBeInTheDocument();
  });
});
