import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../../test/render';
import { helmApi } from './fixtures';
import { RepositoriesPanel } from './RepositoriesPanel';

describe('RepositoriesPanel', () => {
  it('lists the repositories, never a password, and adds and removes session repositories', async () => {
    const api = helmApi({
      extra: {
        'GET /clusters/c/helm/repositories': () =>
          new Response(
            JSON.stringify({
              repositories: [
                { name: 'charts', url: 'https://charts.example.org', scope: 'org', hasAuth: false },
                { name: 'mine', url: 'https://charts.internal', scope: 'session', hasAuth: true },
              ],
            }),
            { headers: { 'Content-Type': 'application/json' } },
          ),
      },
    });
    renderApp(<RepositoriesPanel cluster="c" />, { client: api.client });
    await screen.findByText('charts');
    const list = screen.getByRole('list', { name: 'Repositories' });
    expect(within(list).getByText('charts')).toBeInTheDocument();
    expect(within(list).getByText(/organisation/)).toBeInTheDocument();
    expect(within(list).getAllByRole('button', { name: /Remove/ })).toHaveLength(1);

    await userEvent.type(screen.getByRole('textbox', { name: 'Name' }), 'new');
    await userEvent.type(screen.getByRole('textbox', { name: 'URL' }), 'https://x.example.org');
    await userEvent.type(screen.getByLabelText('Password'), 'pw');
    await userEvent.click(screen.getByRole('button', { name: 'Add repository' }));
    await waitFor(() =>
      expect(api.calls.find((c) => c.method === 'POST' && c.path.endsWith('/repositories'))?.body).toEqual({
        name: 'new',
        url: 'https://x.example.org',
        username: '',
        password: 'pw',
      }),
    );

    await userEvent.click(within(list).getByRole('button', { name: 'Remove mine' }));
    await waitFor(() =>
      expect(api.calls.some((c) => c.method === 'DELETE' && c.path.endsWith('/repositories/mine'))).toBe(
        true,
      ),
    );
  });
});
