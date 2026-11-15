import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { renderApp } from '../../test/render';
import { ClusterRail } from './ClusterRail';
import type { Source } from './useClusters';

const sources: Source[] = [
  {
    type: 'vault',
    label: 'Vault',
    status: 'ok',
    clusters: [
      { id: 'v:a', name: 'a', state: 'open' },
      { id: 'v:b', name: 'b', state: 'closed' },
    ],
  },
];

describe('ClusterRail favourites', () => {
  it('a star button per cluster says whether it is a favourite and toggles it', async () => {
    const onToggle = vi.fn();
    renderApp(<ClusterRail sources={sources} favourites={['v:b']} onToggleFavourite={onToggle} />);
    const fav = screen.getByRole('group', { name: 'Favourites' });
    const pinned = within(fav).getByRole('button', { name: 'Remove b from favourites' });
    expect(pinned).toHaveAttribute('aria-pressed', 'true');
    await userEvent.click(pinned);
    expect(onToggle).toHaveBeenLastCalledWith('v:b', false);
    const add = screen.getByRole('button', { name: 'Add a to favourites' });
    expect(add).toHaveAttribute('aria-pressed', 'false');
    await userEvent.click(add);
    expect(onToggle).toHaveBeenLastCalledWith('v:a', true);
  });

  it('shows no star buttons without a handler', () => {
    renderApp(<ClusterRail sources={sources} favourites={[]} />);
    expect(screen.queryByRole('button', { name: /favourites/ })).toBeNull();
  });
});
