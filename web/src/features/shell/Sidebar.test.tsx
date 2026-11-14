import { screen, waitFor, within } from '@testing-library/react';
import { Route, Routes } from 'react-router';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import type { Discovery, DiscoveryResource } from '../../api/discovery';
import { jsonResponse, renderApp } from '../../test/render';
import { ResourceSidebar, Sidebar } from './Sidebar';

const res = (group: string, kind: string, resource: string, namespaced = true): DiscoveryResource => ({
  group,
  version: 'v1',
  resource,
  kind,
  namespaced,
  verbs: ['list'],
  preferred: true,
  servedVersions: ['v1'],
});

const discovery: Discovery = {
  serverVersion: 'v1.35.0',
  resources: [
    res('', 'Pod', 'pods'),
    res('apps', 'Deployment', 'deployments'),
    res('', 'Secret', 'secrets'),
    res('', 'Service', 'services'),
    res('', 'Node', 'nodes', false),
    res('events.k8s.io', 'Event', 'events'),
    res('', 'PersistentVolumeClaim', 'persistentvolumeclaims'),
  ],
};

const all = () => true;

describe('Sidebar', () => {
  it('shows the sections that have items, in the order of the spec, with the kinds the cluster serves', () => {
    renderApp(<Sidebar cluster="v:prod" discovery={discovery} canList={all} />);
    const headings = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent);
    expect(headings).toEqual(['Workloads', 'Config', 'Network', 'Storage', 'Cluster', 'Pod Files', 'Events']);
    const workloads = within(screen.getByRole('region', { name: 'Workloads' }));
    expect(workloads.getAllByRole('link').map((l) => l.textContent)).toEqual(['Pods', 'Deployments']);
    expect(screen.queryByRole('link', { name: 'StatefulSets' })).toBeNull();
  });

  it('links to the list of the kind, with the cluster ID percent-encoded', () => {
    renderApp(<Sidebar cluster="k:prod/ctx" discovery={discovery} canList={all} />);
    expect(screen.getByRole('link', { name: 'Pods' })).toHaveAttribute('href', '/c/k%3Aprod%2Fctx/core/Pod');
    expect(screen.getByRole('link', { name: 'Deployments' })).toHaveAttribute(
      'href',
      '/c/k%3Aprod%2Fctx/apps/Deployment',
    );
    expect(screen.getByRole('link', { name: 'Pod Files' })).toHaveAttribute(
      'href',
      '/c/k%3Aprod%2Fctx/pod-files',
    );
  });

  it('hides a kind the user cannot list, shows it while the answer is unknown, and hides empty sections', () => {
    const canList = (_g: string, r: string) =>
      r === 'secrets' ? false : r === 'services' ? undefined : true;
    renderApp(<Sidebar cluster="c" discovery={discovery} canList={canList} />);
    expect(screen.queryByRole('link', { name: 'Secrets' })).toBeNull();
    expect(screen.getByRole('link', { name: 'Services' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'ConfigMaps' }), 'not served').toBeNull();
  });

  it('removes a section whose kinds are all hidden', () => {
    renderApp(<Sidebar cluster="c" discovery={discovery} canList={(_g, r) => r !== 'secrets'} />);
    expect(screen.queryByRole('region', { name: 'Config' })).toBeNull();
  });

  it('marks the current item', () => {
    renderApp(<Sidebar cluster="c" discovery={discovery} canList={all} />, { route: '/c/c/apps/Deployment' });
    expect(screen.getByRole('link', { name: 'Deployments' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('link', { name: 'Pods' })).not.toHaveAttribute('aria-current');
  });

  it('shows a loading message until discovery arrives', () => {
    renderApp(<Sidebar cluster="c" discovery={undefined} canList={all} />);
    expect(screen.getByRole('status')).toHaveTextContent('Loading');
  });
});

describe('ResourceSidebar', () => {
  it('asks for discovery and list permissions, then hides what is denied', async () => {
    const asked: { verb: string; resource: string }[][] = [];
    const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith('/clusters/c/discovery')) return Promise.resolve(jsonResponse(discovery));
      const checks = (JSON.parse(String(init?.body)) as { checks: { verb: string; resource: string }[] })
        .checks;
      asked.push(checks);
      return Promise.resolve(
        jsonResponse({ results: checks.map((c) => ({ allowed: c.resource !== 'secrets' })) }),
      );
    });
    renderApp(
      <Routes>
        <Route path="/c/:cluster/*" element={<ResourceSidebar cluster="c" />} />
      </Routes>,
      { client: createClient({ fetch }), route: '/c/c' },
    );
    expect(await screen.findByRole('link', { name: 'Pods' })).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole('link', { name: 'Secrets' })).toBeNull());
    expect(screen.getByRole('link', { name: 'Services' })).toBeInTheDocument();
    expect(asked).toHaveLength(1);
    expect(asked[0]?.every((c) => c.verb === 'list')).toBe(true);
  });
});
