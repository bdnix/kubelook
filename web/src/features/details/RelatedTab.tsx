import { Link } from 'react-router';
import { groupOf, type ChainLink } from '../../lib/owners';
import { resourcePath } from '../../lib/paths';
import { getString } from '../../lib/path';
import { podsForService, servicesOfIngress } from '../../lib/related';
import type { K8sObject } from '../../state/live';

interface Props {
  cluster: string;
  kind: string;
  object: K8sObject;
  chain: ChainLink[];
  /** Objects whose ownerReferences point at this one, from the live lists of the common child kinds. */
  children: K8sObject[];
  pods: K8sObject[];
}

function List({ title, items }: { title: string; items: { key: string; label: string; to: string }[] }) {
  if (items.length === 0) return null;
  return (
    <section aria-label={title}>
      <h3>{title}</h3>
      <ul>
        {items.map((i) => (
          <li key={i.key}>
            <Link to={i.to}>{i.label}</Link>
          </li>
        ))}
      </ul>
    </section>
  );
}

function link(cluster: string, o: K8sObject) {
  const kind = getString(o, 'kind');
  const name = getString(o, 'metadata.name');
  return {
    key: getString(o, 'metadata.uid') || `${kind}/${name}`,
    label: `${kind}/${name}`,
    to: resourcePath({
      cluster,
      namespace: getString(o, 'metadata.namespace'),
      group: groupOf(getString(o, 'apiVersion')),
      kind,
      name,
    }),
  };
}

/** Owners, children, and the Service ↔ pods and Ingress → Services relations. */
export function RelatedTab({ cluster, kind, object, chain, children, pods }: Props) {
  const ns = getString(object, 'metadata.namespace');
  const owners = chain.map(({ ref }) => ({
    key: ref.uid || `${ref.kind}/${ref.name}`,
    label: `${ref.kind}/${ref.name}`,
    to: resourcePath({
      cluster,
      namespace: ns,
      group: groupOf(ref.apiVersion),
      kind: ref.kind,
      name: ref.name,
    }),
  }));
  const services = kind === 'Ingress' ? servicesOfIngress(object) : [];
  const matched = kind === 'Service' ? podsForService(object, pods) : [];
  const empty = owners.length + children.length + services.length + matched.length === 0;
  return (
    <div>
      <List title="Owners" items={owners} />
      <List title="Children" items={children.map((c) => link(cluster, c))} />
      <List title="Pods" items={matched.map((p) => link(cluster, { ...p, kind: 'Pod', apiVersion: 'v1' }))} />
      <List
        title="Services"
        items={services.map((s) => ({
          key: s,
          label: `Service/${s}`,
          to: resourcePath({ cluster, namespace: ns, group: '', kind: 'Service', name: s }),
        }))}
      />
      {empty && <p>Nothing related.</p>}
    </div>
  );
}
