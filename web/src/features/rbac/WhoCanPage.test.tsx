import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { WhoCanPage } from './WhoCanPage';

const discovery = {
  serverVersion: 'v1.35.0',
  resources: [
    { group: '', version: 'v1', resource: 'pods', kind: 'Pod', namespaced: true, verbs: ['list'], preferred: true, servedVersions: ['v1'] },
    { group: 'apps', version: 'v1', resource: 'deployments', kind: 'Deployment', namespaced: true, verbs: ['list'], preferred: true, servedVersions: ['v1'] },
  ],
};
const rule = { apiGroups: [''], resources: ['pods'], verbs: ['delete'] };
const list = (items: unknown[]) => () => jsonResponse({ items, resourceVersion: '1', continue: '' });
const base = '/clusters/v%3Aprod/resources/rbac.authorization.k8s.io/v1';

function mount(extra: Record<string, () => Response> = {}) {
  return renderApp(<WhoCanPage cluster="v:prod" />, {
    route: '/c/v%3Aprod/who-can',
    client: fakeClient({
      'GET /clusters/v%3Aprod/discovery': () => jsonResponse(discovery),
      [`GET ${base}/roles?limit=5000`]: list([]),
      [`GET ${base}/clusterroles?limit=5000`]: list([{ metadata: { name: 'pod-killer' }, rules: [rule] }]),
      [`GET ${base}/rolebindings?limit=5000`]: list([
        { metadata: { name: 'rb', namespace: 'dev' }, roleRef: { kind: 'ClusterRole', name: 'pod-killer' }, subjects: [{ kind: 'ServiceAccount', name: 'builder', namespace: 'dev' }] },
      ]),
      [`GET ${base}/clusterrolebindings?limit=5000`]: list([
        { metadata: { name: 'crb' }, roleRef: { kind: 'ClusterRole', name: 'pod-killer' }, subjects: [{ kind: 'User', name: 'ada' }] },
        { metadata: { name: 'dead' }, roleRef: { kind: 'ClusterRole', name: 'gone' }, subjects: [{ kind: 'User', name: 'bob' }] },
      ]),
      ...extra,
    }),
  });
}

async function ask() {
  await userEvent.selectOptions(await screen.findByLabelText('Verb'), 'delete');
  await userEvent.selectOptions(screen.getByLabelText('Resource'), 'pods');
}

describe('WhoCanPage', () => {
  it('asks for a verb and a resource first, and says what it reads', async () => {
    mount();
    expect(await screen.findByLabelText('Verb')).toBeInTheDocument();
    expect(screen.getByText('Choose a verb and a resource.')).toBeInTheDocument();
    expect(screen.getByText('Other authorizers, if any, are not shown.')).toBeInTheDocument();
  });

  it('offers the verbs and the resources the cluster serves', async () => {
    mount();
    const verbs = await screen.findByLabelText('Verb');
    expect(within(verbs).getAllByRole('option').map((o) => o.textContent)).toEqual(
      expect.arrayContaining(['get', 'list', 'delete', '*']),
    );
    await waitFor(() => expect(within(screen.getByLabelText('Resource')).getAllByRole('option').length).toBeGreaterThan(2));
    expect(within(screen.getByLabelText('Resource')).getAllByRole('option').map((o) => o.textContent)).toEqual(
      expect.arrayContaining(['pods', 'deployments (apps)']),
    );
  });

  it('lists who can, with the scope and the binding and role as links', async () => {
    mount();
    await ask();
    const table = await screen.findByRole('table', { name: 'Who can delete pods' });
    const rows = within(table).getAllByRole('row').slice(1);
    expect(rows.map((r) => within(r).getAllByRole('cell').map((c) => c.textContent))).toEqual([
      ['ServiceAccount dev/builder', 'dev', 'rb', 'pod-killer'],
      ['User ada', 'All namespaces', 'crb', 'pod-killer'],
    ]);
    expect(within(rows[1] as HTMLElement).getByRole('link', { name: 'crb' })).toHaveAttribute(
      'href',
      '/c/v%3Aprod/core/ClusterRoleBinding/crb'.replace('/core/', '/rbac.authorization.k8s.io/'),
    );
    expect(within(rows[0] as HTMLElement).getByRole('link', { name: 'rb' })).toHaveAttribute(
      'href',
      '/c/v%3Aprod/ns/dev/rbac.authorization.k8s.io/RoleBinding/rb',
    );
    expect(within(rows[1] as HTMLElement).getByRole('link', { name: 'pod-killer' })).toHaveAttribute(
      'href',
      '/c/v%3Aprod/rbac.authorization.k8s.io/ClusterRole/pod-killer',
    );
  });

  it('a namespace narrows the answer', async () => {
    mount();
    await ask();
    await screen.findByRole('table', { name: 'Who can delete pods' });
    await userEvent.type(screen.getByLabelText('Namespace'), 'qa');
    const table = await screen.findByRole('table', { name: 'Who can delete pods in qa' });
    expect(within(table).getAllByRole('row').slice(1).map((r) => r.textContent)).toEqual([expect.stringContaining('User ada')]);
  });

  it('lists the bindings whose role is missing', async () => {
    mount();
    await ask();
    const section = await screen.findByRole('region', { name: 'Dangling bindings' });
    expect(within(section).getByText(/crb|dead/)).toBeInTheDocument();
    expect(within(section).getByText(/ClusterRole gone/)).toBeInTheDocument();
  });

  it('says so when nobody matches', async () => {
    mount();
    await userEvent.selectOptions(await screen.findByLabelText('Verb'), 'create');
    await userEvent.selectOptions(screen.getByLabelText('Resource'), 'pods');
    expect(await screen.findByText('No subject has this permission through the bindings you can list.')).toBeInTheDocument();
  });

  it('warns when the user cannot list one of the four kinds', async () => {
    mount({
      [`GET ${base}/clusterrolebindings?limit=5000`]: () =>
        jsonResponse({ error: { code: 'forbidden', message: 'denied' } }, 403),
    });
    await ask();
    expect(await screen.findByText("Results may be incomplete: you can't list ClusterRoleBindings.")).toBeInTheDocument();
    expect(await screen.findByRole('table', { name: 'Who can delete pods' })).toBeInTheDocument();
  });

  it('names every kind that cannot be listed', async () => {
    const denied = () => jsonResponse({ error: { code: 'forbidden', message: 'denied' } }, 403);
    mount({ [`GET ${base}/roles?limit=5000`]: denied, [`GET ${base}/rolebindings?limit=5000`]: denied });
    await ask();
    expect(await screen.findByText("Results may be incomplete: you can't list Roles and RoleBindings.")).toBeInTheDocument();
  });

  it('warns when a list was cut short', async () => {
    mount({ [`GET ${base}/roles?limit=5000`]: () => jsonResponse({ items: [], continue: 'more' }) });
    await ask();
    expect(await screen.findByText('Results may be incomplete: Roles has more than 5000 objects.')).toBeInTheDocument();
  });

  it('shows the restriction of a rule that names objects', async () => {
    mount({
      [`GET ${base}/clusterroles?limit=5000`]: list([
        { metadata: { name: 'pod-killer' }, rules: [{ ...rule, resourceNames: ['web-1'] }] },
      ]),
    });
    await ask();
    expect(await screen.findAllByText(/only named objects: web-1/)).not.toHaveLength(0);
  });
});
