import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { DiscoveryResource } from '../../api/discovery';
import { renderApp } from '../../test/render';
import { CrdSidebarSection } from './CrdSidebarSection';

const res = (group: string, kind: string): DiscoveryResource => ({
  group,
  version: 'v1',
  resource: `${kind.toLowerCase()}s`,
  kind,
  namespaced: true,
  verbs: ['list'],
  preferred: true,
  servedVersions: ['v1'],
});

describe('CrdSidebarSection', () => {
  it('lists the kinds under a heading for each API group, with links to their lists', () => {
    renderApp(
      <CrdSidebarSection
        cluster="v:prod"
        kinds={[res('a.io', 'Alpha'), res('a.io', 'Beta'), res('b.io', 'Gamma')]}
      />,
    );
    const a = within(screen.getByRole('group', { name: 'a.io' }));
    expect(a.getAllByRole('link').map((l) => l.textContent)).toEqual(['Alpha', 'Beta']);
    expect(a.getByRole('link', { name: 'Alpha' })).toHaveAttribute('href', '/c/v%3Aprod/a.io/Alpha');
    expect(within(screen.getByRole('group', { name: 'b.io' })).getAllByRole('link')).toHaveLength(1);
  });
});
