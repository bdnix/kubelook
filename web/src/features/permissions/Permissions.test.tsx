import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { describe, expect, it } from 'vitest';
import type { RbacData } from '../../lib/rbac';
import { GatekeeperConstraints, gatekeeperSummary } from './GatekeeperConstraints';
import { KyvernoPolicies, kyvernoViolations } from './KyvernoPolicies';
import { ServiceAccountPermissionsView } from './ServiceAccountPermissions';

const wrap = (ui: React.ReactElement) => render(<MemoryRouter>{ui}</MemoryRouter>);

describe('ServiceAccountPermissionsView', () => {
  const data: RbacData = {
    roles: [],
    clusterRoles: [
      { metadata: { name: 'secrets' }, rules: [{ verbs: ['get', 'list'], resources: ['secrets'], apiGroups: [''] }] },
      { metadata: { name: 'view' }, rules: [{ verbs: ['get'], resources: ['pods'], apiGroups: [''] }] },
    ],
    roleBindings: [],
    clusterRoleBindings: [
      { metadata: { name: 'a' }, roleRef: { kind: 'ClusterRole', name: 'secrets' }, subjects: [{ kind: 'ServiceAccount', name: 'zed', namespace: 'shop' }] },
      { metadata: { name: 'b' }, roleRef: { kind: 'ClusterRole', name: 'view' }, subjects: [{ kind: 'ServiceAccount', name: 'alpha', namespace: 'shop' }] },
    ],
  };
  const accounts = [
    { metadata: { name: 'alpha', namespace: 'shop' } },
    { metadata: { name: 'zed', namespace: 'shop' } },
    { metadata: { name: 'bare', namespace: 'shop' } },
  ];

  it('sorts the accounts by risk and then by name, and says why', () => {
    wrap(<ServiceAccountPermissionsView cluster="c" accounts={accounts} data={data} forbidden={[]} truncated={[]} />);
    const rows = within(screen.getByRole('table', { name: 'ServiceAccounts' })).getAllByRole('row').slice(1);
    expect(rows.map((r) => within(r).getAllByRole('cell')[1]?.textContent)).toEqual(['zed', 'alpha', 'bare']);
    expect(within(rows[0] as HTMLElement).getAllByRole('cell')[0]).toHaveTextContent('critical');
    expect(rows[0]).toHaveTextContent('get, list secrets in all namespaces');
    expect(within(rows[1] as HTMLElement).getAllByRole('cell')[0]).toHaveTextContent('low');
    expect(within(rows[0] as HTMLElement).getByRole('link', { name: 'zed' })).toHaveAttribute('href', '/c/c/ns/shop/core/ServiceAccount/zed');
  });

  it('says the levels are based on RBAC rules only, and that some kinds could not be read', () => {
    wrap(<ServiceAccountPermissionsView cluster="c" accounts={accounts} data={data} forbidden={['ClusterRoleBindings']} truncated={['Roles']} />);
    expect(screen.getByText('Based on RBAC rules only.')).toBeInTheDocument();
    expect(screen.getByText(/You may not list ClusterRoleBindings/)).toBeInTheDocument();
    expect(screen.getByText(/Roles was cut off/)).toBeInTheDocument();
  });

  it('has no action buttons', () => {
    wrap(<ServiceAccountPermissionsView cluster="c" accounts={accounts} data={data} forbidden={[]} truncated={[]} />);
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });
});

describe('Kyverno', () => {
  const policies = [
    { kind: 'ClusterPolicy', object: { metadata: { name: 'no-latest' }, spec: { validationFailureAction: 'Enforce' }, status: { conditions: [{ type: 'Ready', status: 'True' }] } } },
    { kind: 'Policy', object: { metadata: { name: 'labels', namespace: 'shop' }, spec: { validationFailureAction: 'Audit' }, status: { conditions: [{ type: 'Ready', status: 'False' }] } } },
  ];
  const reports = [
    { metadata: { name: 'r1', namespace: 'shop' }, results: [{ policy: 'no-latest', result: 'fail', message: 'latest tag' }, { policy: 'no-latest', result: 'pass' }, { policy: 'labels', result: 'fail', message: 'missing label' }] },
    { metadata: { name: 'r2' }, results: [{ policy: 'no-latest', result: 'fail' }] },
  ];
  it('groups the failed results by policy', () => {
    expect(kyvernoViolations(reports)).toEqual({ labels: 1, 'no-latest': 2 });
  });
  it('lists the policies with readiness, action and violations, and notes the source is unconfirmed', () => {
    wrap(<KyvernoPolicies policies={policies} reports={reports} />);
    const rows = within(screen.getByRole('table', { name: 'Kyverno policies' })).getAllByRole('row').slice(1);
    expect(rows[0]).toHaveTextContent('no-latest');
    expect(rows[0]).toHaveTextContent('Enforce');
    expect(rows[0]).toHaveTextContent('2');
    expect(rows[1]).toHaveTextContent('labels');
    expect(rows[1]).toHaveTextContent('False');
    expect(screen.getByText(/unconfirmed until the spike finding is recorded/)).toBeInTheDocument();
  });
});

describe('Gatekeeper', () => {
  const constraint = (total: number, listed: number) => ({
    kind: 'K8sRequiredLabels',
    object: {
      metadata: { name: 'must-have-owner' },
      spec: { enforcementAction: 'deny' },
      status: { totalViolations: total, violations: Array.from({ length: listed }, (_, i) => ({ name: `pod-${i}`, namespace: 'shop', kind: 'Pod', message: 'missing owner' })) },
    },
  });
  it('summarises a constraint', () => {
    expect(gatekeeperSummary(constraint(3, 3).object)).toEqual({ action: 'deny', total: 3, listed: 3, cut: false });
    expect(gatekeeperSummary(constraint(80, 20).object)).toEqual({ action: 'deny', total: 80, listed: 20, cut: true });
  });
  it('says when the list of violations is cut short', () => {
    wrap(<GatekeeperConstraints constraints={[constraint(80, 20)]} />);
    expect(screen.getByText('Showing 20 of 80 violations (Gatekeeper keeps a limited number per constraint).')).toBeInTheDocument();
    expect(screen.getByText('must-have-owner')).toBeInTheDocument();
    expect(screen.getByText(/K8sRequiredLabels/)).toBeInTheDocument();
  });
  it('shows no cut-off note when every violation is listed', () => {
    wrap(<GatekeeperConstraints constraints={[constraint(2, 2)]} />);
    expect(screen.queryByText(/Showing/)).toBeNull();
  });
});
