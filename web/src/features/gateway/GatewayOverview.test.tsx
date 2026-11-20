import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { GatewayOverview, gatewayState } from './GatewayOverview';

const obj = (conditions: { type: string; status: string }[]) => ({ metadata: { name: 'x', namespace: 'ns' }, status: { conditions } });

describe('gatewayState', () => {
  it.each([
    [[{ type: 'Accepted', status: 'True' }, { type: 'Programmed', status: 'True' }], 'ready'],
    [[{ type: 'Accepted', status: 'True' }, { type: 'Programmed', status: 'False' }], 'notReady'],
    [[{ type: 'Accepted', status: 'Unknown' }, { type: 'Programmed', status: 'True' }], 'inProgress'],
    [[{ type: 'Ready', status: 'True' }], 'ready'],
    [[{ type: 'Accepted', status: 'False' }, { type: 'Programmed', status: 'Unknown' }], 'notReady'],
    [[{ type: 'Other', status: 'True' }], 'unknown'],
    [[], 'unknown'],
  ] as const)('%j → %s', (conditions, want) => {
    expect(gatewayState(obj([...conditions]))).toBe(want);
  });
});

describe('GatewayOverview', () => {
  it('counts the objects of each kind by state and lists the events', () => {
    render(
      <GatewayOverview
        now={Date.parse('2026-11-20T10:00:00Z')}
        kinds={[
          { kind: 'Gateway', objects: [obj([{ type: 'Programmed', status: 'True' }]), obj([{ type: 'Programmed', status: 'False' }]), obj([])] },
          { kind: 'HTTPRoute', objects: [obj([{ type: 'Accepted', status: 'Unknown' }])] },
        ]}
        events={[{ reason: 'Synced', message: 'gateway synced', type: 'Normal', lastTimestamp: '2026-11-20T09:55:00Z', involvedObject: { kind: 'Gateway', name: 'gw' } }]}
      />,
    );
    const gw = screen.getByRole('row', { name: /Gateway/ });
    expect(within(gw).getAllByRole('cell').map((c) => c.textContent)).toEqual(['Gateway', '1', '1', '0', '1']);
    const route = screen.getByRole('row', { name: /HTTPRoute/ });
    expect(within(route).getAllByRole('cell').map((c) => c.textContent)).toEqual(['HTTPRoute', '0', '0', '1', '0']);
    expect(screen.getByRole('list', { name: 'Events' })).toHaveTextContent('gateway synced');
  });

  it('says so without kinds and without events', () => {
    render(<GatewayOverview kinds={[]} events={[]} now={0} />);
    expect(screen.getByText('This cluster serves no Gateway API kinds.')).toBeInTheDocument();
  });
});
