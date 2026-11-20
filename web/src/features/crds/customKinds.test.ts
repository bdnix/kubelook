import { describe, expect, it } from 'vitest';
import type { Discovery, DiscoveryResource } from '../../api/discovery';
import { customKinds, kindTemplate } from './customKinds';

const res = (group: string, kind: string, over: Partial<DiscoveryResource> = {}): DiscoveryResource => ({
  group,
  version: 'v1',
  resource: `${kind.toLowerCase()}s`,
  kind,
  namespaced: true,
  verbs: ['get', 'list', 'watch'],
  preferred: true,
  servedVersions: ['v1'],
  ...over,
});

const discovery = (...resources: DiscoveryResource[]): Discovery => ({ serverVersion: 'v1.35.0', resources });

describe('customKinds', () => {
  it('keeps kinds of groups that are not built into Kubernetes', () => {
    const d = discovery(
      res('', 'Pod'),
      res('apps', 'Deployment'),
      res('networking.k8s.io', 'Ingress'),
      res('apiextensions.k8s.io', 'CustomResourceDefinition'),
      res('argoproj.io', 'Application'),
      res('gateway.networking.k8s.io', 'HTTPRoute'),
    );
    expect(customKinds(d).map((k) => `${k.group}/${k.kind}`)).toEqual([
      'argoproj.io/Application',
      'gateway.networking.k8s.io/HTTPRoute',
    ]);
  });

  it('leaves out kinds that a module claims', () => {
    const d = discovery(res('keda.sh', 'ScaledObject'), res('keda.sh', 'ScaledJob'));
    const claimed = (group: string, kind: string) => group === 'keda.sh' && kind === 'ScaledObject';
    expect(customKinds(d, claimed).map((k) => k.kind)).toEqual(['ScaledJob']);
  });

  it('leaves out kinds that cannot be listed and versions that are not preferred', () => {
    const d = discovery(
      res('a.io', 'NoList', { verbs: ['get'] }),
      res('a.io', 'Old', { preferred: false }),
      res('a.io', 'Fine'),
    );
    expect(customKinds(d).map((k) => k.kind)).toEqual(['Fine']);
  });

  it('sorts by group, then kind', () => {
    const d = discovery(res('z.io', 'B'), res('a.io', 'Z'), res('a.io', 'A'));
    expect(customKinds(d).map((k) => `${k.group}/${k.kind}`)).toEqual(['a.io/A', 'a.io/Z', 'z.io/B']);
  });
});

describe('kindTemplate', () => {
  it('has apiVersion, kind and a name, plus a namespace for a namespaced kind', () => {
    expect(kindTemplate(res('gateway.networking.k8s.io', 'HTTPRoute', { version: 'v1' }))).toBe(
      'apiVersion: gateway.networking.k8s.io/v1\nkind: HTTPRoute\nmetadata:\n  name: example\n  namespace: default\n',
    );
  });

  it('has no namespace for a cluster-scoped kind', () => {
    expect(kindTemplate(res('karpenter.sh', 'NodePool', { namespaced: false }))).toBe(
      'apiVersion: karpenter.sh/v1\nkind: NodePool\nmetadata:\n  name: example\n',
    );
  });
});
