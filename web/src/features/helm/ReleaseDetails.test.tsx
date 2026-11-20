import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { renderApp } from '../../test/render';
import { helmApi } from './fixtures';
import { ReleaseDetails } from './ReleaseDetails';

vi.mock('../../adapters/monaco/YamlEditor', () => ({
  YamlEditor: (p: { value: string; onChange: (v: string) => void; label: string }) => (
    <textarea aria-label={p.label} value={p.value} onChange={(e) => p.onChange(e.target.value)} />
  ),
}));

function mount(api = helmApi(), onClose = vi.fn()) {
  renderApp(<ReleaseDetails cluster="c" namespace="team-a" name="web" onClose={onClose} />, { client: api.client });
  return { api, onClose };
}

const tab = (name: string) => screen.findByRole('tab', { name });

describe('ReleaseDetails', () => {
  it('shows the values hidden until Reveal, which calls the audited endpoint, and hides them again', async () => {
    const { api } = mount();
    await userEvent.click(await tab('Values'));
    expect(screen.queryByText(/replicas: 3/)).toBeNull();
    expect(api.calls.some((c) => c.path.endsWith('/values/reveal'))).toBe(false);
    await userEvent.click(screen.getByRole('button', { name: 'Reveal values' }));
    expect(await screen.findByText(/message: hello/)).toBeInTheDocument();
    expect(api.calls.filter((c) => c.path.endsWith('/values/reveal'))).toHaveLength(1);
    await userEvent.click(screen.getByRole('button', { name: 'Hide values' }));
    expect(screen.queryByText(/message: hello/)).toBeNull();
  });

  it('hides the values again when the details are closed and opened again', async () => {
    const api = helmApi();
    const { unmount } = renderApp(<ReleaseDetails cluster="c" namespace="team-a" name="web" onClose={vi.fn()} />, { client: api.client });
    await userEvent.click(await tab('Values'));
    await userEvent.click(screen.getByRole('button', { name: 'Reveal values' }));
    await screen.findByText(/message: hello/);
    unmount();
    renderApp(<ReleaseDetails cluster="c" namespace="team-a" name="web" onClose={vi.fn()} />, { client: api.client });
    await userEvent.click(await tab('Values'));
    expect(screen.queryByText(/message: hello/)).toBeNull();
  });

  it('shows the manifest, the notes as text, the hooks and the history', async () => {
    mount();
    await userEvent.click(await tab('Manifest'));
    expect(screen.getByText(/kind: ConfigMap/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('tab', { name: 'Notes' }));
    expect(screen.getByText(/Thank you <b>for<\/b> installing web/)).toBeInTheDocument();
    expect(document.querySelector('b')).toBeNull();
    await userEvent.click(screen.getByRole('tab', { name: 'Hooks' }));
    expect(screen.getByText('web-test')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('tab', { name: 'History' }));
    const rows = within(screen.getByRole('table', { name: 'History' })).getAllByRole('row');
    expect(rows).toHaveLength(3);
    expect(rows[1]).toHaveTextContent('Upgrade complete');
  });

  it('uninstall and test ask first, then start a job', async () => {
    const { api } = mount();
    await screen.findByRole('tab', { name: 'Values' });
    await userEvent.click(screen.getByRole('button', { name: 'Uninstall' }));
    const dialog = await screen.findByRole('dialog', { name: 'Uninstall web' });
    expect(api.calls.some((c) => c.path.endsWith('/jobs'))).toBe(false);
    await userEvent.click(within(dialog).getByRole('button', { name: 'Uninstall' }));
    await waitFor(() => expect(api.calls.find((c) => c.path.endsWith('/jobs'))?.body).toEqual({ action: 'uninstall', namespace: 'team-a', name: 'web' }));
    await userEvent.click(screen.getByRole('button', { name: 'Test' }));
    await userEvent.click(within(await screen.findByRole('dialog', { name: 'Test web' })).getByRole('button', { name: 'Run test' }));
    await waitFor(() => expect(api.calls.filter((c) => c.path.endsWith('/jobs')).at(-1)?.body).toEqual({ action: 'test', namespace: 'team-a', name: 'web' }));
  });

  it('rollback picks a revision and starts a job', async () => {
    const { api } = mount();
    await screen.findByRole('tab', { name: 'Values' });
    await userEvent.click(screen.getByRole('button', { name: 'Roll back' }));
    const dialog = await screen.findByRole('dialog', { name: 'Roll back web' });
    expect(within(dialog).getByRole('button', { name: 'Roll back' })).toBeDisabled();
    await userEvent.selectOptions(within(dialog).getByRole('combobox', { name: 'Revision' }), '1');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Roll back' }));
    await waitFor(() => expect(api.calls.find((c) => c.path.endsWith('/jobs'))?.body).toEqual({ action: 'rollback', namespace: 'team-a', name: 'web', revision: 1 }));
  });

  it('disables the actions, with the reason, when the user may not create Secrets in the namespace', async () => {
    mount(helmApi({ allowed: false }));
    const uninstall = await screen.findByRole('button', { name: 'Uninstall' });
    await waitFor(() => expect(uninstall).toBeDisabled());
    expect(screen.getByText(/You may not create Secrets in team-a/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Upgrade' })).toBeDisabled();
  });

  it('shows an error when the release cannot be read', async () => {
    mount(helmApi({ extra: {} }));
    expect(await screen.findByRole('tab', { name: 'Values' })).toBeInTheDocument();
  });
});
