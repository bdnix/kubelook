import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, makeWrapper } from '../../test/render';
import { useSchema } from './useSchema';

const doc = {
  components: {
    schemas: {
      Dep: { 'x-kubernetes-group-version-kind': [{ group: 'apps', version: 'v1', kind: 'Deployment' }] },
    },
  },
};

describe('useSchema', () => {
  it('requests the document of the group and version and passes it on with the ref of the kind', async () => {
    const fetch = vi.fn(() => Promise.resolve(jsonResponse(doc)));
    const { result } = renderHook(() => useSchema('v:prod', 'apps', 'v1', 'Deployment'), {
      wrapper: makeWrapper(createClient({ fetch })),
    });
    await waitFor(() => expect(result.current).toBeDefined());
    expect(fetch).toHaveBeenCalledWith('/api/v1/clusters/v%3Aprod/openapi/apps/v1', expect.anything());
    expect(result.current).toEqual({ schema: doc, schemaRef: '#/components/schemas/Dep' });
  });

  it('writes the empty group as core', async () => {
    const fetch = vi.fn(() => Promise.resolve(jsonResponse({})));
    const { result } = renderHook(() => useSchema('c', '', 'v1', 'Pod'), {
      wrapper: makeWrapper(createClient({ fetch })),
    });
    await waitFor(() => expect(result.current).toBeDefined());
    expect(fetch).toHaveBeenCalledWith('/api/v1/clusters/c/openapi/core/v1', expect.anything());
    expect(result.current?.schemaRef).toBeUndefined();
  });

  it('is undefined while loading, when disabled and when the request fails', async () => {
    const fetch = vi.fn(() =>
      Promise.resolve(jsonResponse({ error: { code: 'not_found', message: 'x' } }, 404)),
    );
    const { result } = renderHook(() => useSchema('c', 'apps', 'v1', 'Deployment'), {
      wrapper: makeWrapper(createClient({ fetch })),
    });
    expect(result.current).toBeUndefined();
    await waitFor(() => expect(fetch).toHaveBeenCalled());
    expect(result.current).toBeUndefined();

    const idle = vi.fn(() => Promise.resolve(jsonResponse(doc)));
    renderHook(() => useSchema('c', 'apps', 'v1', 'Deployment', false), {
      wrapper: makeWrapper(createClient({ fetch: idle })),
    });
    expect(idle).not.toHaveBeenCalled();
  });
});
