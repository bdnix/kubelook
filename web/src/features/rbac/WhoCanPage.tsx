import { useMemo, useState } from 'react';
import { Link } from 'react-router';
import { useDiscovery } from '../../api/discovery';
import { whoCan, type Grant } from '../../lib/rbac';
import { resourcePath } from '../../lib/paths';
import { useRbacData, RBAC_LIMIT } from './useRbacData';

const VERBS = ['get', 'list', 'watch', 'create', 'update', 'patch', 'delete', 'deletecollection', '*'];
const RBAC_GROUP = 'rbac.authorization.k8s.io';

const joinNames = (names: string[]) =>
  names.length <= 1 ? names.join('') : `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;

function Cell({ cluster, grant, kind }: { cluster: string; grant: Grant; kind: 'binding' | 'role' }) {
  const r = grant[kind];
  const to = resourcePath({ cluster, namespace: r.namespace, group: RBAC_GROUP, kind: r.kind, name: r.name });
  return <Link to={to}>{r.name}</Link>;
}

/** "Who can <verb> <resource>?", answered from the roles and bindings the user can list. */
export function WhoCanPage({ cluster }: { cluster: string }) {
  const discovery = useDiscovery(cluster);
  const rbac = useRbacData(cluster);
  const [verb, setVerb] = useState('');
  const [target, setTarget] = useState(''); // "<group>|<resource>"
  const [namespace, setNamespace] = useState('');

  const resources = useMemo(
    () =>
      (discovery.data?.resources ?? [])
        .filter((r) => r.preferred)
        .map((r) => ({
          value: `${r.group}|${r.resource}`,
          label: r.group ? `${r.resource} (${r.group})` : r.resource,
        }))
        .sort((a, b) => a.label.localeCompare(b.label)),
    [discovery.data],
  );
  const [group = '', resource = ''] = target.split('|');
  const ready = verb !== '' && resource !== '';
  const result = useMemo(
    () =>
      ready && !rbac.loading
        ? whoCan({ verb, group, resource, namespace: namespace.trim() }, rbac.data)
        : undefined,
    [ready, rbac.loading, rbac.data, verb, group, resource, namespace],
  );
  const title = `Who can ${verb} ${resource}${namespace.trim() ? ` in ${namespace.trim()}` : ''}`;
  const incomplete = [
    ...(rbac.forbidden.length > 0 ? [`you can't list ${joinNames(rbac.forbidden)}`] : []),
    ...rbac.truncated.map((k) => `${k} has more than ${RBAC_LIMIT} objects`),
    ...(rbac.failed.length > 0 ? [`${joinNames(rbac.failed)} could not be read`] : []),
  ];

  return (
    <>
      <h1>Who can…</h1>
      <label>
        Verb
        <select value={verb} onChange={(e) => setVerb(e.target.value)}>
          <option value="">Choose a verb</option>
          {VERBS.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </select>
      </label>
      <label>
        Resource
        <select value={target} onChange={(e) => setTarget(e.target.value)}>
          <option value="">Choose a resource</option>
          {resources.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </select>
      </label>
      <label>
        Namespace
        <input
          value={namespace}
          onChange={(e) => setNamespace(e.target.value)}
          placeholder="all namespaces"
        />
      </label>
      {incomplete.map((text) => (
        <p key={text} role="status">{`Results may be incomplete: ${text}.`}</p>
      ))}
      <p>Other authorizers, if any, are not shown.</p>
      {!ready && <p>Choose a verb and a resource.</p>}
      {ready && rbac.loading && <p role="status">Loading…</p>}
      {result && result.subjects.length === 0 && (
        <p>No subject has this permission through the bindings you can list.</p>
      )}
      {result && result.subjects.length > 0 && (
        <table aria-label={title}>
          <thead>
            <tr>
              <th>Subject</th>
              <th>Scope</th>
              <th>Binding</th>
              <th>Role</th>
            </tr>
          </thead>
          <tbody>
            {result.subjects.flatMap((s) =>
              s.grants.map((g, i) => (
                <tr key={`${s.kind}|${s.namespace ?? ''}|${s.name}|${i}`}>
                  <td>{`${s.kind} ${s.namespace ? `${s.namespace}/` : ''}${s.name}`}</td>
                  <td>{g.scope === 'all' ? 'All namespaces' : g.scope}</td>
                  <td>
                    <Cell cluster={cluster} grant={g} kind="binding" />
                  </td>
                  <td>
                    <Cell cluster={cluster} grant={g} kind="role" />
                    {g.onlyNamed && <small>{` only named objects: ${g.onlyNamed.join(', ')}`}</small>}
                  </td>
                </tr>
              )),
            )}
          </tbody>
        </table>
      )}
      {result && result.dangling.length > 0 && (
        <section aria-label="Dangling bindings">
          <h2>Dangling bindings</h2>
          <p>These bindings point at a role that does not exist.</p>
          <ul>
            {result.dangling.map((d) => (
              <li key={`${d.binding.kind}|${d.binding.namespace ?? ''}|${d.binding.name}`}>
                {`${d.binding.kind} ${d.binding.namespace ? `${d.binding.namespace}/` : ''}${d.binding.name} → ${d.role.kind} ${d.role.name}`}
              </li>
            ))}
          </ul>
        </section>
      )}
    </>
  );
}
