import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, makeWrapper } from '../../test/render';
import { useCrd } from './useCrd';

const crd = {
  spec: { group: 'example.com', names: { kind: 'Widget', plural: 'widgets' }, scope: 'Namespaced' },
};

describe('useCrd', () => {
  it('reads the CRD named <plural>.<group>', async () => {
    const fetch = vi.fn(() => Promise.resolve(jsonResponse(crd)));
    const { result } = renderHook(() => useCrd('v:prod', 'example.com', 'widgets'), {
      wrapper: makeWrapper(createClient({ fetch })),
    });
    await waitFor(() => expect(result.current).toEqual(crd));
    expect(fetch).toHaveBeenCalledWith(
      '/api/v1/clusters/v%3Aprod/resources/apiextensions.k8s.io/v1/customresourcedefinitions/widgets.example.com',
      expect.anything(),
    );
  });

  it('is undefined when the user may not read CRDs', async () => {
    const fetch = vi.fn(() =>
      Promise.resolve(jsonResponse({ error: { code: 'forbidden', message: 'no' } }, 403)),
    );
    const { result } = renderHook(() => useCrd('c', 'example.com', 'widgets'), {
      wrapper: makeWrapper(createClient({ fetch })),
    });
    await waitFor(() => expect(fetch).toHaveBeenCalled());
    expect(result.current).toBeUndefined();
  });

  it('does not ask while disabled', () => {
    const fetch = vi.fn(() => Promise.resolve(jsonResponse(crd)));
    renderHook(() => useCrd('c', 'example.com', 'widgets', false), {
      wrapper: makeWrapper(createClient({ fetch })),
    });
    expect(fetch).not.toHaveBeenCalled();
  });
});
