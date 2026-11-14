import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes, useLocation } from 'react-router';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../../test/render';
import { ClusterRail } from './ClusterRail';
import type { Source } from './useClusters';

const sources: Source[] = [
  {
    type: 'vault',
    label: 'Vault',
    status: 'ok',
    clusters: [
      { id: 'v:a', name: 'a', roles: ['devops-role'], state: 'open' },
      { id: 'v:b', name: 'b', roles: ['devops-role', 'platform-hub-role'], state: 'closed' },
    ],
  },
  {
    type: 'kubeconfig',
    label: 'prod',
    status: 'ok',
    clusters: [
      { id: 'k:prod/ctx1', name: 'ctx1', state: 'closed', server: 'https://one.example' },
      { id: 'k:prod/ctx2', name: 'ctx2', state: 'disconnected' },
    ],
  },
];

function Location() {
  return <p data-testid="location">{useLocation().pathname}</p>;
}

describe('ClusterRail', () => {
  it('groups clusters by source under the headings "Vault" and each kubeconfig label', () => {
    renderApp(<ClusterRail sources={sources} favourites={[]} />);
    expect(screen.getByRole('heading', { name: 'Vault' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'prod' })).toBeInTheDocument();
    const vault = screen.getByRole('group', { name: 'Vault' });
    expect(within(vault).getAllByRole('link').map((l) => l.textContent)).toEqual(['a', 'b']);
  });

  it('labels each status dot for assistive technology', () => {
    renderApp(<ClusterRail sources={sources} favourites={[]} />);
    expect(within(screen.getByRole('link', { name: 'a' })).getByRole('img', { name: 'connected' })).toBeInTheDocument();
    expect(within(screen.getByRole('link', { name: 'b' })).getByRole('img', { name: 'not opened' })).toBeInTheDocument();
    expect(within(screen.getByRole('link', { name: 'ctx2' })).getByRole('img', { name: 'disconnected' })).toBeInTheDocument();
  });

  it('pins favourites on top, once', () => {
    renderApp(<ClusterRail sources={sources} favourites={['k:prod/ctx2', 'v:gone']} />);
    const headings = screen.getAllByRole('heading').map((h) => h.textContent);
    expect(headings).toEqual(['Favourites', 'Vault', 'prod']);
    expect(within(screen.getByRole('group', { name: 'Favourites' })).getAllByRole('link').map((l) => l.textContent)).toEqual(['ctx2']);
    expect(screen.getAllByRole('link', { name: 'ctx2' })).toHaveLength(1);
  });

  it('has no favourites group when none of them is in the list', () => {
    renderApp(<ClusterRail sources={sources} favourites={['v:gone']} />);
    expect(screen.queryByRole('heading', { name: 'Favourites' })).toBeNull();
  });

  it('navigates to the percent-encoded cluster path on click', async () => {
    const user = userEvent.setup();
    renderApp(
      <>
        <ClusterRail sources={sources} favourites={[]} />
        <Routes>
          <Route path="*" element={<Location />} />
        </Routes>
      </>,
    );
    await user.click(screen.getByRole('link', { name: 'ctx1' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/c/k%3Aprod%2Fctx1');
  });

  it('shows a role picker only for a Vault cluster with more than one role', async () => {
    const user = userEvent.setup();
    renderApp(<ClusterRail sources={sources} favourites={[]} />);
    expect(screen.queryByLabelText('Role for a')).toBeNull();
    const picker = screen.getByLabelText('Role for b');
    expect(picker).toHaveValue('devops-role');
    await user.selectOptions(picker, 'platform-hub-role');
    expect(picker).toHaveValue('platform-hub-role');
  });

  it('honours a selected role the server reports', () => {
    const withRole: Source[] = [
      { type: 'vault', label: 'Vault', status: 'ok', clusters: [{ id: 'v:b', name: 'b', roles: ['x', 'y'], selectedRole: 'y', state: 'closed' }] },
    ];
    renderApp(<ClusterRail sources={withRole} favourites={[]} />);
    expect(screen.getByLabelText('Role for b')).toHaveValue('y');
  });

  it('says so when there are no clusters', () => {
    renderApp(<ClusterRail sources={[]} favourites={[]} />);
    expect(screen.getByText('No clusters yet.')).toBeInTheDocument();
  });

  it('renders cluster-supplied names as text, never as markup', () => {
    const evil: Source[] = [{ type: 'kubeconfig', label: '<i>x</i>', status: 'ok', clusters: [{ id: 'k:l/c', name: '<b>c</b>', state: 'closed' }] }];
    renderApp(<ClusterRail sources={evil} favourites={[]} />);
    expect(screen.getByRole('link', { name: '<b>c</b>' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: '<i>x</i>' })).toBeInTheDocument();
  });
});
