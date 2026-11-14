import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { dockStore } from '../../state/dock';
import { jsonResponse, renderApp } from '../../test/render';
import { DebugButton } from './DebugButton';
import { DebugDialog } from './DebugDialog';
import { NodeShellButton } from './NodeShellButton';

const pod = {
  metadata: { name: 'web-1', namespace: 'ns' },
  spec: { containers: [{ name: 'app' }, { name: 'side' }], initContainers: [{ name: 'setup' }] },
};

const meBody = (nodeShell: boolean) => ({
  user: { id: 'u', provider: 'google', email: 'a@b', name: 'A' },
  csrfToken: 'c',
  settings: { favouriteClusters: null, watchIdleSeconds: 30, theme: 'light' },
  maintenanceBanner: '',
  features: { kubeconfig: true, fileBrowser: true, nodeShell, vault: true, helmVersion: 4, modules: {} },
  limits: {
    fileTransferBytes: 1,
    logsHistoryMaxLines: 1,
    logsLiveBufferLines: 1,
    vaultExpiryWarningSeconds: 1,
  },
  vault: null,
  keyId: 'k',
});

interface Req {
  method: string;
  url: string;
  body?: unknown;
}

function client(opts: { allowed?: boolean; nodeShell?: boolean; fail?: string } = {}) {
  const sent: Req[] = [];
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    const body: unknown = init?.body ? JSON.parse(String(init.body)) : undefined;
    const method = init?.method ?? 'GET';
    if (url.endsWith('/me')) return Promise.resolve(jsonResponse(meBody(opts.nodeShell ?? true)));
    if (url.endsWith('/access'))
      return Promise.resolve(jsonResponse({ results: [{ allowed: opts.allowed ?? true }] }));
    sent.push({ method, url, body });
    if (opts.fail)
      return Promise.resolve(jsonResponse({ error: { code: 'forbidden', message: opts.fail } }, 403));
    if (url.endsWith('/debug/node'))
      return Promise.resolve(
        jsonResponse({ namespace: 'default', pod: 'kubelook-node-shell-abcde', container: 'shell' }),
      );
    return Promise.resolve(jsonResponse({ container: 'kubelook-debug-abcde', reused: false }));
  });
  return { client: createClient({ fetch }), sent };
}

beforeEach(() => dockStore.setState({ tabs: [], active: null }));

describe('DebugDialog', () => {
  it('warns that the container stays, offers profiles and targets (init containers too), and opens a shell in the new container', async () => {
    const { client: c, sent } = client();
    const onClose = vi.fn();
    renderApp(<DebugDialog cluster="v:prod" pod={pod} onClose={onClose} />, { client: c });
    expect(
      screen.getByText(
        "A debug container stays in the pod until the pod is deleted. Kubernetes doesn't allow removing it.",
      ),
    ).toBeInTheDocument();
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual([
      'general',
      'baseline',
      'restricted',
      'netadmin',
      'sysadmin',
      'app',
      'side',
      'setup',
    ]);
    await userEvent.selectOptions(screen.getByLabelText('Profile'), 'netadmin');
    await userEvent.selectOptions(screen.getByLabelText('Target container'), 'side');
    await userEvent.click(screen.getByRole('button', { name: 'Add debug container' }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(sent[0]).toEqual({
      method: 'POST',
      url: '/api/v1/clusters/v%3Aprod/debug/pod',
      body: { namespace: 'ns', pod: 'web-1', target: 'side', profile: 'netadmin' },
    });
    expect(dockStore.getState().tabs[0]).toMatchObject({
      kind: 'shell',
      props: { pod: 'web-1', container: 'kubelook-debug-abcde' },
    });
  });

  it('a single-container pod has no target picker', () => {
    renderApp(
      <DebugDialog
        cluster="c"
        pod={{ metadata: { name: 'p', namespace: 'n' }, spec: { containers: [{ name: 'only' }] } }}
        onClose={() => undefined}
      />,
    );
    expect(screen.queryByLabelText('Target container')).toBeNull();
  });

  it('shows a refusal as text and keeps the dialog; Cancel and Escape close', async () => {
    const { client: c } = client({ fail: 'pods/ephemeralcontainers is forbidden' });
    const onClose = vi.fn();
    renderApp(<DebugDialog cluster="c" pod={pod} onClose={onClose} />, { client: c });
    await userEvent.click(screen.getByRole('button', { name: 'Add debug container' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('pods/ephemeralcontainers is forbidden');
    expect(onClose).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    screen.getByRole('dialog').focus();
    await userEvent.keyboard('{Escape}');
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(dockStore.getState().tabs).toHaveLength(0);
  });

  it('reports a network failure in general words', async () => {
    const fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
    renderApp(<DebugDialog cluster="c" pod={pod} onClose={() => undefined} />, {
      client: createClient({ fetch }),
    });
    await userEvent.click(screen.getByRole('button', { name: 'Add debug container' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('The server could not be reached.');
  });
});

describe('DebugButton', () => {
  it('is hidden without patch on pods/ephemeralcontainers and opens the dialog with it', async () => {
    const denied = client({ allowed: false });
    const { unmount } = renderApp(<DebugButton cluster="c" pod={pod} />, { client: denied.client });
    await waitFor(() => expect(denied.client).toBeDefined());
    expect(screen.queryByRole('button', { name: 'Debug…' })).toBeNull();
    unmount();
    renderApp(<DebugButton cluster="c" pod={pod} />, { client: client().client });
    await userEvent.click(await screen.findByRole('button', { name: 'Debug…' }));
    expect(screen.getByRole('dialog', { name: 'Debug web-1' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});

describe('NodeShellButton', () => {
  const node = { metadata: { name: 'n1' } };

  it('starts a node shell and opens a titled shell tab marked as a node shell', async () => {
    const { client: c, sent } = client();
    renderApp(<NodeShellButton cluster="v:prod" node={node} />, { client: c });
    await userEvent.click(await screen.findByRole('button', { name: 'Node shell' }));
    await waitFor(() => expect(dockStore.getState().tabs).toHaveLength(1));
    expect(sent[0]).toEqual({
      method: 'POST',
      url: '/api/v1/clusters/v%3Aprod/debug/node',
      body: { node: 'n1' },
    });
    expect(dockStore.getState().tabs[0]).toMatchObject({
      kind: 'shell',
      title: 'Node shell: n1',
      props: { namespace: 'default', pod: 'kubelook-node-shell-abcde', container: 'shell', nodeShell: true },
    });
  });

  it('is hidden when the feature is off or the user may not exec', async () => {
    const off = client({ nodeShell: false });
    const { unmount } = renderApp(<NodeShellButton cluster="c" node={node} />, { client: off.client });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByRole('button', { name: 'Node shell' })).toBeNull();
    unmount();
    const denied = client({ allowed: false });
    renderApp(<NodeShellButton cluster="c" node={node} />, { client: denied.client });
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByRole('button', { name: 'Node shell' })).toBeNull();
  });

  it('shows a refusal as text', async () => {
    const failing = createClient({
      fetch: (url: string) =>
        Promise.resolve(
          url.endsWith('/me')
            ? jsonResponse(meBody(true))
            : url.endsWith('/access')
              ? jsonResponse({ results: [{ allowed: true }] })
              : jsonResponse({ error: { code: 'not_found', message: 'node shells are not enabled' } }, 404),
        ),
    });
    renderApp(<NodeShellButton cluster="c" node={node} />, { client: failing });
    await userEvent.click(await screen.findByRole('button', { name: 'Node shell' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('node shells are not enabled');
  });

  it('a network failure is reported in general words', async () => {
    const flaky = createClient({
      fetch: (url: string) =>
        url.endsWith('/me')
          ? Promise.resolve(jsonResponse(meBody(true)))
          : url.endsWith('/access')
            ? Promise.resolve(jsonResponse({ results: [{ allowed: true }] }))
            : Promise.reject(new TypeError('offline')),
    });
    renderApp(<NodeShellButton cluster="c" node={node} />, { client: flaky });
    await userEvent.click(await screen.findByRole('button', { name: 'Node shell' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('The server could not be reached.');
  });
});
