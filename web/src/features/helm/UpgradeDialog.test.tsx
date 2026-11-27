import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { renderApp } from '../../test/render';
import { detail, helmApi } from '../../test/helm';
import { InstallDialog } from './InstallDialog';
import { UpgradeDialog } from './UpgradeDialog';

vi.mock('../../adapters/monaco/YamlEditor', () => ({
  YamlEditor: (p: { value: string; onChange: (v: string) => void; label: string }) => (
    <textarea aria-label={p.label} value={p.value} onChange={(e) => p.onChange(e.target.value)} />
  ),
}));

describe('UpgradeDialog', () => {
  function mount(api = helmApi()) {
    const onClose = vi.fn();
    renderApp(<UpgradeDialog cluster="c" release={detail() as never} onClose={onClose} />, {
      client: api.client,
    });
    return { api, onClose };
  }

  it('starts with the current values, shows both diffs and enables Confirm only after the preview', async () => {
    const { api } = mount();
    const editor = await screen.findByRole('textbox', { name: 'Values (YAML)' });
    await waitFor(() => expect(editor).toHaveValue('replicas: 3\n'));
    expect(api.calls.filter((c) => c.path.endsWith('/values/reveal'))).toHaveLength(1);
    expect(screen.getByRole('button', { name: 'Confirm upgrade' })).toBeDisabled();

    await screen.findByRole('option', { name: 'charts' });
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Repository' }), 'charts');
    await userEvent.clear(screen.getByRole('textbox', { name: 'Chart version' }));
    await userEvent.type(screen.getByRole('textbox', { name: 'Chart version' }), '1.2.0');
    await userEvent.clear(editor);
    await userEvent.click(editor);
    await userEvent.paste('replicas: 5\n');
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }));

    expect(await screen.findByRole('region', { name: 'Values diff' })).toHaveTextContent('replicas: 5');
    expect(screen.getByRole('region', { name: 'Manifest diff' })).toHaveTextContent('replicas: "5"');
    expect(api.calls.find((c) => c.path.endsWith('/preview'))?.body).toEqual({
      action: 'upgrade',
      namespace: 'team-a',
      name: 'web',
      chart: { repo: 'charts', name: 'web', version: '1.2.0' },
      values: { replicas: 5 },
    });
    expect(screen.getByRole('button', { name: 'Confirm upgrade' })).toBeEnabled();

    await userEvent.click(screen.getByRole('button', { name: 'Confirm upgrade' }));
    await waitFor(() =>
      expect(api.calls.find((c) => c.path.endsWith('/jobs'))?.body).toMatchObject({
        action: 'upgrade',
        values: { replicas: 5 },
      }),
    );
  });

  it('turns Confirm off again when an input changes after the preview', async () => {
    mount();
    const editor = await screen.findByRole('textbox', { name: 'Values (YAML)' });
    await waitFor(() => expect(editor).toHaveValue('replicas: 3\n'));
    await screen.findByRole('option', { name: 'charts' });
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Repository' }), 'charts');
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }));
    await screen.findByRole('region', { name: 'Values diff' });
    expect(screen.getByRole('button', { name: 'Confirm upgrade' })).toBeEnabled();
    await userEvent.type(editor, 'x');
    expect(screen.getByRole('button', { name: 'Confirm upgrade' })).toBeDisabled();
  });

  it('refuses values that are not a YAML mapping', async () => {
    mount();
    const editor = await screen.findByRole('textbox', { name: 'Values (YAML)' });
    await waitFor(() => expect(editor).toHaveValue('replicas: 3\n'));
    await screen.findByRole('option', { name: 'charts' });
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Repository' }), 'charts');
    await userEvent.clear(editor);
    await userEvent.click(editor);
    await userEvent.paste('- a\n- b\n');
    expect(screen.getByRole('alert')).toHaveTextContent(/mapping/);
    expect(screen.getByRole('button', { name: 'Preview' })).toBeDisabled();
  });

  it('shows the error of a failed preview', async () => {
    const api = helmApi();
    api.fetch.mockImplementationOnce(api.fetch.getMockImplementation() as never);
    mount(
      helmApi({
        extra: {
          'POST /clusters/c/helm/preview': () =>
            new Response(
              JSON.stringify({ error: { code: 'not_found', message: 'The repository has no chart web.' } }),
              { status: 404, headers: { 'Content-Type': 'application/json' } },
            ),
        },
      }),
    );
    await screen.findByRole('option', { name: 'charts' });
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Repository' }), 'charts');
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect(await screen.findByText('The repository has no chart web.')).toBeInTheDocument();
  });
});

describe('InstallDialog', () => {
  function mount(api = helmApi()) {
    renderApp(<InstallDialog cluster="c" onClose={vi.fn()} />, { client: api.client });
    return api;
  }

  it('installs from a repository chart', async () => {
    const api = mount();
    await screen.findByRole('option', { name: 'charts' });
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Repository' }), 'charts');
    await userEvent.type(screen.getByRole('textbox', { name: 'Chart' }), 'web');
    await userEvent.type(screen.getByRole('textbox', { name: 'Chart version' }), '1.0.0');
    await userEvent.type(screen.getByRole('textbox', { name: 'Namespace' }), 'team-b');
    await userEvent.type(screen.getByRole('textbox', { name: 'Release name' }), 'api');
    await userEvent.click(screen.getByRole('button', { name: 'Install' }));
    await waitFor(() =>
      expect(api.calls.find((c) => c.path.endsWith('/jobs'))?.body).toEqual({
        action: 'install',
        namespace: 'team-b',
        name: 'api',
        chart: { repo: 'charts', name: 'web', version: '1.0.0' },
        values: {},
      }),
    );
  });

  it('installs from an OCI reference', async () => {
    const api = mount();
    await screen.findByRole('option', { name: 'charts' });
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Repository' }), 'oci');
    await userEvent.type(
      screen.getByRole('textbox', { name: 'OCI reference' }),
      'oci://registry.example.com/charts/web',
    );
    await userEvent.type(screen.getByRole('textbox', { name: 'Namespace' }), 'team-b');
    await userEvent.type(screen.getByRole('textbox', { name: 'Release name' }), 'api');
    await userEvent.click(screen.getByRole('button', { name: 'Install' }));
    await waitFor(() =>
      expect(api.calls.find((c) => c.path.endsWith('/jobs'))?.body).toMatchObject({
        chart: { oci: 'oci://registry.example.com/charts/web' },
      }),
    );
  });

  it('keeps Install off until the form is complete', async () => {
    mount();
    expect(await screen.findByRole('button', { name: 'Install' })).toBeDisabled();
  });
});
