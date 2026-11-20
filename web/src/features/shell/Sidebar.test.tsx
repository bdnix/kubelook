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
    expect(headings).toEqual([
      'Workloads',
      'Config',
      'Network',
      'Storage',
      'Cluster',
      'Access Control',
      'Helm',
      'Pod Files',
      'Events',
    ]);
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
    expect(screen.getByRole('link', { name: 'Who can…' })).toHaveAttribute(
      'href',
      '/c/k%3Aprod%2Fctx/who-can',
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

  it('lists the custom kinds the cluster serves under Custom Resources, by API group, and hides the ones the user cannot list', () => {
    const withCrds: Discovery = {
      ...discovery,
      resources: [...discovery.resources, res('a.io', 'Alpha', 'alphas'), res('a.io', 'Hidden', 'hiddens')],
    };
    renderApp(<Sidebar cluster="c" discovery={withCrds} modules={[]} canList={(_g, r) => r !== 'hiddens'} />);
    const section = within(screen.getByRole('region', { name: 'Custom Resources' }));
    expect(section.getAllByRole('link').map((l) => l.textContent)).toEqual(['Alpha']);
    expect(section.getByRole('link', { name: 'Alpha' })).toHaveAttribute('href', '/c/c/a.io/Alpha');
    expect(screen.queryByRole('region', { name: 'Custom Resources' })).not.toBeNull();
  });

  it('has no Custom Resources section on a cluster without custom kinds', () => {
    renderApp(<Sidebar cluster="c" discovery={discovery} modules={[]} canList={all} />);
    expect(screen.queryByRole('region', { name: 'Custom Resources' })).toBeNull();
  });

  describe('modules', () => {
    const withKeda: Discovery = {
      ...discovery,
      resources: [
        ...discovery.resources,
        res('keda.sh', 'ScaledObject', 'scaledobjects'),
        res('keda.sh', 'ScaledJob', 'scaledjobs'),
      ],
    };
    const mod = (name: string, active: boolean, kinds: [string, string, string][] = []) => ({
      name,
      active,
      kinds: kinds.map(([group, kind, resource]) => ({
        group,
        version: 'v1',
        resource,
        kind,
        namespaced: true,
      })),
    });
    const modules = [
      mod('keda', true, [['keda.sh', 'ScaledObject', 'scaledobjects']]),
      mod('trivy', false),
      mod('permissions', true),
    ];

    it('shows the active modules, Permissions under Security, and hides the inactive ones', () => {
      renderApp(<Sidebar cluster="c" discovery={withKeda} modules={modules} canList={all} />);
      const section = within(screen.getByRole('region', { name: 'Modules' }));
      expect(section.getByRole('link', { name: 'KEDA' })).toHaveAttribute('href', '/c/c/modules/keda');
      const security = within(section.getByRole('group', { name: 'Security' }));
      expect(security.getAllByRole('link').map((l) => l.textContent)).toEqual(['Permissions and policy']);
      expect(section.queryByRole('link', { name: 'Trivy Operator' })).toBeNull();
    });

    it('leaves the kinds of an active module out of Custom Resources, and keeps the other kinds of its group', () => {
      renderApp(<Sidebar cluster="c" discovery={withKeda} modules={modules} canList={all} />);
      const custom = within(screen.getByRole('region', { name: 'Custom Resources' }));
      expect(custom.getAllByRole('link').map((l) => l.textContent)).toEqual(['ScaledJob']);
    });

    it('lists the kinds of an inactive module under Custom Resources', () => {
      renderApp(
        <Sidebar
          cluster="c"
          discovery={withKeda}
          modules={[mod('keda', false), mod('permissions', true)]}
          canList={all}
        />,
      );
      expect(screen.queryByRole('link', { name: 'KEDA' })).toBeNull();
      const custom = within(screen.getByRole('region', { name: 'Custom Resources' }));
      expect(custom.getAllByRole('link').map((l) => l.textContent)).toEqual(['ScaledJob', 'ScaledObject']);
    });

    it('waits for the modules before it lists custom kinds, so claimed kinds never flash there', () => {
      renderApp(<Sidebar cluster="c" discovery={withKeda} modules={undefined} canList={all} />);
      expect(screen.queryByRole('region', { name: 'Custom Resources' })).toBeNull();
    });
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

  it('also asks about the custom kinds', async () => {
    const withCrds: Discovery = {
      ...discovery,
      resources: [...discovery.resources, res('a.io', 'Alpha', 'alphas')],
    };
    const asked: { resource: string }[] = [];
    const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).endsWith('/clusters/c/discovery')) return Promise.resolve(jsonResponse(withCrds));
      const checks = (JSON.parse(String(init?.body)) as { checks: { resource: string }[] }).checks;
      asked.push(...checks);
      return Promise.resolve(
        jsonResponse({ results: checks.map((c) => ({ allowed: c.resource !== 'alphas' })) }),
      );
    });
    renderApp(
      <Routes>
        <Route path="/c/:cluster/*" element={<ResourceSidebar cluster="c" />} />
      </Routes>,
      { client: createClient({ fetch }), route: '/c/c' },
    );
    await screen.findByRole('link', { name: 'Pods' });
    await waitFor(() => expect(asked.some((c) => c.resource === 'alphas')).toBe(true));
    await waitFor(() => expect(screen.queryByRole('link', { name: 'Alpha' })).toBeNull());
  });
});

describe('Sidebar and Flux', () => {
  const fluxDiscovery: Discovery = {
    serverVersion: 'v1.35.0',
    resources: [res('kustomize.toolkit.fluxcd.io', 'Kustomization', 'kustomizations')],
  };
  const flux = (active: boolean) => ({
    name: 'flux',
    active,
    kinds: active
      ? [
          {
            group: 'kustomize.toolkit.fluxcd.io',
            version: 'v1',
            resource: 'kustomizations',
            kind: 'Kustomization',
            namespaced: true,
          },
        ]
      : [],
  });

  it('shows the dashboard and the Flux kinds under FluxCD, and not under Custom Resources, when Flux is active', () => {
    renderApp(<Sidebar cluster="c" discovery={fluxDiscovery} modules={[flux(true)]} canList={() => true} />);
    const section = within(screen.getByRole('region', { name: 'FluxCD' }));
    expect(section.getByRole('link', { name: 'Dashboard' })).toHaveAttribute('href', '/c/c/flux');
    expect(section.getByRole('link', { name: 'Kustomization' })).toHaveAttribute(
      'href',
      '/c/c/kustomize.toolkit.fluxcd.io/Kustomization',
    );
    expect(screen.queryByRole('region', { name: 'Custom Resources' })).toBeNull();
  });

  it('has no dashboard where Flux is not active', () => {
    renderApp(<Sidebar cluster="c" discovery={discovery} modules={[flux(false)]} canList={() => true} />);
    expect(screen.queryByRole('link', { name: 'Dashboard' })).toBeNull();
  });
});
