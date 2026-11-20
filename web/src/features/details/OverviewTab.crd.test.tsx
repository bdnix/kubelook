import { screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, renderApp } from '../../test/render';
import { OverviewTab } from './OverviewTab';

const widget = {
  metadata: { name: 'w', namespace: 'ns' },
  spec: { replicas: 3 },
  status: { conditions: [{ type: 'Ready', status: 'True' }] },
};

describe('OverviewTab for custom resources', () => {
  it('shows spec and the conditions table', () => {
    const fetch = vi.fn(() => Promise.resolve(jsonResponse({}, 404)));
    renderApp(
      <OverviewTab
        cluster="c"
        group="example.com"
        kind="Widget"
        object={widget}
        now={0}
        chain={[]}
        pods={[]}
      />,
      { client: createClient({ fetch }) },
    );
    expect(screen.getByRole('region', { name: 'Spec' })).toBeInTheDocument();
    expect(screen.getByRole('table', { name: 'Conditions' })).toBeInTheDocument();
  });

  it('does not add a spec tree to built-in kinds', () => {
    renderApp(
      <OverviewTab cluster="c" group="apps" kind="Deployment" object={widget} now={0} chain={[]} pods={[]} />,
    );
    expect(screen.queryByRole('region', { name: 'Spec' })).toBeNull();
  });
});
