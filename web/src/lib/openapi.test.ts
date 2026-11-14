import { describe, expect, it } from 'vitest';
import { findSchemaRef } from './openapi';

const doc = {
  components: {
    schemas: {
      'io.k8s.api.apps.v1.Deployment': {
        'x-kubernetes-group-version-kind': [{ group: 'apps', version: 'v1', kind: 'Deployment' }],
      },
      'io.k8s.api.core.v1.Pod': {
        'x-kubernetes-group-version-kind': [{ group: '', version: 'v1', kind: 'Pod' }],
      },
      'io.k8s.api.core.v1.PodSpec': {},
    },
  },
};

describe('findSchemaRef', () => {
  it('finds a kind by group, version and kind', () => {
    expect(findSchemaRef(doc, 'apps', 'v1', 'Deployment')).toBe(
      '#/components/schemas/io.k8s.api.apps.v1.Deployment',
    );
  });

  it('treats core and the empty string as the same group', () => {
    expect(findSchemaRef(doc, 'core', 'v1', 'Pod')).toBe('#/components/schemas/io.k8s.api.core.v1.Pod');
    expect(findSchemaRef(doc, '', 'v1', 'Pod')).toBe('#/components/schemas/io.k8s.api.core.v1.Pod');
  });

  it('answers undefined for unknown kinds and for documents without schemas', () => {
    expect(findSchemaRef(doc, 'apps', 'v1', 'Nope')).toBeUndefined();
    expect(findSchemaRef(doc, 'apps', 'v2', 'Deployment')).toBeUndefined();
    expect(findSchemaRef({}, 'apps', 'v1', 'Deployment')).toBeUndefined();
    expect(findSchemaRef(null, 'apps', 'v1', 'Deployment')).toBeUndefined();
  });
});
