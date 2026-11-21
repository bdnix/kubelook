import { describe, expect, it } from 'vitest';
import { buildFluxGraph, type Inventories } from './graph';

const meta = (name: string, namespace = 'flux-system') => ({ name, namespace });
const git = (name: string, ready = 'True') => ({
  apiVersion: 'source.toolkit.fluxcd.io/v1',
  kind: 'GitRepository',
  metadata: meta(name),
  status: { conditions: [{ type: 'Ready', status: ready }], artifact: { revision: 'main@sha1:abc' } },
});
const ks = (name: string, spec: Record<string, unknown>, ready = 'True') => ({
  apiVersion: 'kustomize.toolkit.fluxcd.io/v1',
  kind: 'Kustomization',
  metadata: meta(name),
  spec,
  status: {
    conditions: [{ type: 'Ready', status: ready, message: ready === 'False' ? 'build failed' : 'ok' }],
    lastAppliedRevision: 'main@sha1:abc',
  },
});
const hr = (name: string, spec: Record<string, unknown>) => ({
  apiVersion: 'helm.toolkit.fluxcd.io/v2',
  kind: 'HelmRelease',
  metadata: meta(name),
  spec,
  status: { conditions: [{ type: 'Ready', status: 'True' }] },
});

const edgeSet = (g: ReturnType<typeof buildFluxGraph>) => g.edges.map((e) => `${e.from} -> ${e.to}`).sort();

describe('buildFluxGraph', () => {
  it('draws edges from sourceRef, chart sourceRef, chartRef and dependsOn', () => {
    const g = buildFluxGraph([
      git('repo'),
      { apiVersion: 'source.toolkit.fluxcd.io/v1', kind: 'HelmRepository', metadata: meta('charts') },
      { apiVersion: 'source.toolkit.fluxcd.io/v1', kind: 'HelmChart', metadata: meta('chart') },
      ks('base', { sourceRef: { kind: 'GitRepository', name: 'repo' } }),
      ks('apps', {
        sourceRef: { kind: 'GitRepository', name: 'repo', namespace: 'flux-system' },
        dependsOn: [{ name: 'base' }],
      }),
      hr('web', { chart: { spec: { chart: 'web', sourceRef: { kind: 'HelmRepository', name: 'charts' } } } }),
      hr('api', { chartRef: { kind: 'HelmChart', name: 'chart', namespace: 'flux-system' } }),
    ]);
    expect(edgeSet(g)).toEqual(
      [
        'source.toolkit.fluxcd.io/GitRepository/flux-system/repo -> kustomize.toolkit.fluxcd.io/Kustomization/flux-system/apps',
        'source.toolkit.fluxcd.io/GitRepository/flux-system/repo -> kustomize.toolkit.fluxcd.io/Kustomization/flux-system/base',
        'source.toolkit.fluxcd.io/HelmChart/flux-system/chart -> helm.toolkit.fluxcd.io/HelmRelease/flux-system/api',
        'source.toolkit.fluxcd.io/HelmRepository/flux-system/charts -> helm.toolkit.fluxcd.io/HelmRelease/flux-system/web',
        'kustomize.toolkit.fluxcd.io/Kustomization/flux-system/base -> kustomize.toolkit.fluxcd.io/Kustomization/flux-system/apps'.replace(
          /^/,
          '',
        ),
      ].sort(),
    );
  });

  it('adds the workloads of the inventory and links them from their Kustomization or HelmRelease', () => {
    const inv: Inventories = new Map([
      [
        'kustomize.toolkit.fluxcd.io/Kustomization/flux-system/apps',
        [
          { group: 'apps', version: 'v1', kind: 'Deployment', namespace: 'shop', name: 'web' },
          { group: '', version: 'v1', kind: 'Service', namespace: 'shop', name: 'web' },
        ],
      ],
    ]);
    const g = buildFluxGraph([ks('apps', { sourceRef: { kind: 'GitRepository', name: 'repo' } })], inv);
    const workload = g.nodes.find((n) => n.kind === 'Deployment');
    expect(workload).toMatchObject({ name: 'web' });
    expect(g.nodes.find((n) => n.kind === 'Service')).toBeUndefined();
    expect(edgeSet(g)).toContain(
      'kustomize.toolkit.fluxcd.io/Kustomization/flux-system/apps -> apps/Deployment/shop/web',
    );
  });

  it('flags failed nodes and gives each node its facts', () => {
    const g = buildFluxGraph([
      git('repo'),
      ks('apps', { sourceRef: { kind: 'GitRepository', name: 'repo' } }, 'False'),
    ]);
    const apps = g.nodes.find((n) => n.name === 'apps');
    expect(apps?.status).toBe('error');
    expect(apps?.facts).toContain('Ready=False: build failed');
    expect(apps?.facts).toContain('Revision main@sha1:abc');
    expect(g.nodes.find((n) => n.name === 'repo')?.status).toBe('ok');
  });

  it('shows a suspended object as in progress', () => {
    const g = buildFluxGraph([{ ...git('repo'), spec: { suspend: true } }]);
    expect(g.nodes[0]?.status).toBe('warning');
    expect(g.nodes[0]?.facts).toContain('Suspended');
  });

  it('creates a placeholder for a source the lists do not hold, and does not draw edges to nothing', () => {
    const g = buildFluxGraph([ks('apps', { sourceRef: { kind: 'GitRepository', name: 'gone' } })]);
    expect(g.nodes.find((n) => n.name === 'gone')).toMatchObject({ status: 'unknown' });
    expect(buildFluxGraph([ks('x', { dependsOn: [{ name: 'nope' }] })]).edges).toEqual([]);
  });

  it('is empty for no objects', () => {
    expect(buildFluxGraph([])).toEqual({ nodes: [], edges: [] });
  });
});
