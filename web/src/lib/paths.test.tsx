import { renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter, Route, Routes, useParams } from 'react-router';
import { describe, expect, it } from 'vitest';
import { clusterPath, resourcePath } from './paths';

describe('clusterPath', () => {
  it('percent-encodes every character that is special in a path segment', () => {
    expect(clusterPath('k:prod/ctx')).toBe('/c/k%3Aprod%2Fctx');
    expect(clusterPath('v:prod')).toBe('/c/v%3Aprod');
  });

  it.each(['v:prod', 'k:file/ctx', 'k:a b/c%d', 'k:ünï/ctx?x#y'])(
    'round-trips %j through the router',
    (id) => {
      const wrapper = ({ children }: { children: ReactNode }) => (
        <MemoryRouter initialEntries={[clusterPath(id)]}>
          <Routes>
            <Route path="/c/:cluster/*" element={children} />
          </Routes>
        </MemoryRouter>
      );
      const { result } = renderHook(() => useParams()['cluster'], { wrapper });
      expect(result.current).toBe(id);
    },
  );
});

describe('resourcePath', () => {
  it('builds the namespaced deep link', () => {
    expect(
      resourcePath({ cluster: 'v:prod', namespace: 'web', group: 'apps', kind: 'Deployment', name: 'api' }),
    ).toBe('/c/v%3Aprod/ns/web/apps/Deployment/api');
  });

  it('omits the namespace part for cluster-scoped kinds', () => {
    expect(
      resourcePath({
        cluster: 'v:prod',
        group: 'rbac.authorization.k8s.io',
        kind: 'ClusterRole',
        name: 'admin',
      }),
    ).toBe('/c/v%3Aprod/rbac.authorization.k8s.io/ClusterRole/admin');
    expect(resourcePath({ cluster: 'v:prod', namespace: '', group: '', kind: 'Node', name: 'n1' })).toBe(
      '/c/v%3Aprod/core/Node/n1',
    );
  });

  it('writes the empty (core) group as core', () => {
    expect(resourcePath({ cluster: 'c', namespace: 'default', group: '', kind: 'Pod', name: 'p' })).toBe(
      '/c/c/ns/default/core/Pod/p',
    );
  });

  it('encodes every segment', () => {
    expect(resourcePath({ cluster: 'k:f/c', namespace: 'a b', group: 'g', kind: 'K', name: 'x/y?z' })).toBe(
      '/c/k%3Af%2Fc/ns/a%20b/g/K/x%2Fy%3Fz',
    );
  });
});
