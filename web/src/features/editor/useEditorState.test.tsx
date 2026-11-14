import { act, renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import type { K8sObject } from '../../state/live';
import { jsonResponse, makeWrapper } from '../../test/render';
import { IDENTITY_MESSAGE, useEditorState, type EditTarget } from './useEditorState';

const target: EditTarget = {
  cluster: 'v:prod',
  group: '',
  version: 'v1',
  resource: 'configmaps',
  namespace: 'ns',
  name: 'cm',
};
const cm = (rv: string, a = '1', extra: Record<string, unknown> = {}): K8sObject => ({
  apiVersion: 'v1',
  kind: 'ConfigMap',
  metadata: { name: 'cm', namespace: 'ns', resourceVersion: rv, managedFields: [{ manager: 'x' }] },
  data: { a },
  status: { ignored: true },
  ...extra,
});

type Responder = (url: string, init?: RequestInit) => Response;

function setup(initial: K8sObject, respond: Responder = () => jsonResponse(cm('9', '2'))) {
  const fetch = vi.fn((url: string, init?: RequestInit) => Promise.resolve(respond(url, init)));
  const wrapper = makeWrapper(createClient({ fetch }));
  const hook = renderHook(({ object }) => useEditorState(target, object), {
    wrapper,
    initialProps: { object: initial },
  });
  return { fetch, ...hook };
}

describe('useEditorState', () => {
  it('shows the object as YAML without status and managedFields, keeping the resourceVersion', () => {
    const { result } = setup(cm('7'));
    expect(result.current.text).toContain('resourceVersion: "7"');
    expect(result.current.text).not.toContain('status');
    expect(result.current.text).not.toContain('managedFields');
    expect(result.current.dirty).toBe(false);
    expect(result.current.valid).toBe(true);
  });

  it('tracks dirty state and goes clean again when the text returns to the original', () => {
    const { result } = setup(cm('7'));
    const original = result.current.text;
    act(() => result.current.edit(original.replace('a: "1"', 'a: "2"')));
    expect(result.current.dirty).toBe(true);
    act(() => result.current.edit(original));
    expect(result.current.dirty).toBe(false);
  });

  it('follows a newer version silently while clean', () => {
    const { result, rerender } = setup(cm('7'));
    rerender({ object: cm('8', 'newer') });
    expect(result.current.text).toContain('a: newer');
    expect(result.current.remoteChanged).toBe(false);
    expect(result.current.dirty).toBe(false);
  });

  it('keeps my text and offers the newer version when dirty; Reload takes it', () => {
    const { result, rerender } = setup(cm('7'));
    act(() => result.current.edit(result.current.text.replace('a: "1"', 'a: mine')));
    rerender({ object: cm('8', 'theirs') });
    expect(result.current.remoteChanged).toBe(true);
    expect(result.current.text).toContain('a: mine');
    expect(result.current.latestText).toContain('a: theirs');
    act(() => result.current.setShowDiff(true));
    expect(result.current.showDiff).toBe(true);
    act(() => result.current.reload());
    expect(result.current.text).toContain('a: theirs');
    expect(result.current.remoteChanged).toBe(false);
    expect(result.current.dirty).toBe(false);
    expect(result.current.showDiff).toBe(false);
  });

  it('reports invalid YAML and changed identity, and then cannot be saved', () => {
    const { result } = setup(cm('7'));
    act(() => result.current.edit('a: ['));
    expect(result.current.valid).toBe(false);
    expect(result.current.parseError).toBeTruthy();
    for (const change of [
      (t: string) => t.replace('name: cm', 'name: other'),
      (t: string) => t.replace('namespace: ns', 'namespace: other'),
      (t: string) => t.replace('kind: ConfigMap', 'kind: Secret'),
    ]) {
      act(() => result.current.edit(change(toTextOf(cm('7')))));
      expect(result.current.identityError).toBe(IDENTITY_MESSAGE);
      expect(result.current.valid).toBe(false);
    }
  });

  it('Preview sends a dry-run PUT and turns live and result into a line diff', async () => {
    const respond: Responder = () => jsonResponse({ live: cm('7', '1'), result: cm('7', '2') });
    const { result, fetch } = setup(cm('7'), respond);
    act(() => result.current.edit(result.current.text.replace('a: "1"', 'a: "2"')));
    await act(() => result.current.preview());
    const [url, init] = fetch.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/v1/clusters/v%3Aprod/resources/core/v1/configmaps/cm?namespace=ns&dryRun=true');
    expect(init.method).toBe('PUT');
    expect(JSON.parse(String(init.body))).toMatchObject({ data: { a: '2' } });
    expect(result.current.save.phase).toBe('previewed');
    expect(
      result.current.previewDiff?.filter((l) => l.type !== 'same').map((l) => `${l.type}:${l.text.trim()}`),
    ).toEqual(['del:a: "1"', 'add:a: "2"']);
  });

  it('a cluster-scoped object puts dryRun first in the query', async () => {
    const fetch = vi.fn(() => Promise.resolve(jsonResponse({ live: {}, result: {} })));
    const node: K8sObject = {
      apiVersion: 'v1',
      kind: 'Node',
      metadata: { name: 'n1', resourceVersion: '1' },
    };
    const { result } = renderHook(
      () => useEditorState({ ...target, resource: 'nodes', namespace: '', name: 'n1' }, node),
      {
        wrapper: makeWrapper(createClient({ fetch })),
      },
    );
    await act(() => result.current.preview());
    expect(fetch).toHaveBeenCalledWith(
      '/api/v1/clusters/v%3Aprod/resources/core/v1/nodes/n1?dryRun=true',
      expect.anything(),
    );
  });

  it('shows an admission error from the preview as text', async () => {
    const { result } = setup(cm('7'), () =>
      jsonResponse({ error: { code: 'unprocessable', message: 'webhook denied it' } }, 422),
    );
    await act(() => result.current.preview());
    expect(result.current.save).toMatchObject({
      phase: 'error',
      error: { message: 'webhook denied it' },
      previewUnsupported: false,
    });
  });

  it('dry_run_unsupported marks the preview as unavailable', async () => {
    const { result } = setup(cm('7'), () =>
      jsonResponse({ error: { code: 'dry_run_unsupported', message: 'no' } }, 422),
    );
    await act(() => result.current.preview());
    expect(result.current.save.previewUnsupported).toBe(true);
  });

  it('Save sends the PUT and reloads from the result', async () => {
    const { result, fetch } = setup(cm('7'), () => jsonResponse(cm('9', 'saved')));
    act(() => result.current.edit(result.current.text.replace('a: "1"', 'a: saved')));
    await act(() => result.current.commit());
    expect(fetch.mock.calls[0]?.[0]).toBe(
      '/api/v1/clusters/v%3Aprod/resources/core/v1/configmaps/cm?namespace=ns',
    );
    await waitFor(() => expect(result.current.save.phase).toBe('saved'));
    expect(result.current.text).toContain('resourceVersion: "9"');
    expect(result.current.dirty).toBe(false);
  });

  it('a 409 is a conflict', async () => {
    const { result } = setup(cm('7'), () =>
      jsonResponse({ error: { code: 'conflict', message: 'The object changed since you loaded it.' } }, 409),
    );
    await act(() => result.current.commit());
    expect(result.current.save).toMatchObject({
      phase: 'conflict',
      error: { message: 'The object changed since you loaded it.' },
    });
  });

  it('a network failure is an internal error with a generic text', async () => {
    const fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
    const { result } = renderHook(() => useEditorState(target, cm('7')), {
      wrapper: makeWrapper(createClient({ fetch })),
    });
    await act(() => result.current.commit());
    expect(result.current.save.phase).toBe('error');
  });

  it('does nothing when the text is not valid YAML', async () => {
    const { result, fetch } = setup(cm('7'));
    act(() => result.current.edit('a: ['));
    await act(async () => {
      await result.current.preview();
      await result.current.commit();
    });
    expect(fetch).not.toHaveBeenCalled();
  });

  it('editing after a preview clears it', async () => {
    const { result } = setup(cm('7'), () => jsonResponse({ live: cm('7'), result: cm('7', '2') }));
    await act(() => result.current.preview());
    expect(result.current.previewDiff).toBeDefined();
    act(() => result.current.edit('kind: ConfigMap'));
    expect(result.current.save.phase).toBe('idle');
    expect(result.current.previewDiff).toBeUndefined();
  });
});

function toTextOf(o: K8sObject): string {
  return `apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n  namespace: ns\n  resourceVersion: "${String((o.metadata as { resourceVersion: string }).resourceVersion)}"\ndata:\n  a: "1"\n`;
}
