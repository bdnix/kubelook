import { useState } from 'react';
import { Link } from 'react-router';
import { SafeLink } from '../../components/SafeLink';
import { labelString } from '../../kinds/columns';
import { formatBytes, parseQuantity } from '../../lib/quantity';
import { get, getArray, getString } from '../../lib/path';
import { groupOf, type ChainLink } from '../../lib/owners';
import { resourcePath } from '../../lib/paths';
import { podsForService, podsMountingClaim, servicesOfIngress } from '../../lib/related';
import type { K8sObject } from '../../state/live';
import { CertificateDetails, mayHoldCertificates } from '../secrets/CertificateDetails';
import { SecretData } from '../secrets/SecretData';
import { Conditions } from './Conditions';
import { ContainerList } from './ContainerList';
import { OwnerChain } from './OwnerChain';

const MAX_VALUE = 200;
const WORKLOADS = ['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet'];

function Annotation({ name, value }: { name: string; value: string }) {
  const [open, setOpen] = useState(false);
  const long = value.length > MAX_VALUE;
  const text = long && !open ? `${value.slice(0, MAX_VALUE)}…` : value;
  const isUrl = /^https?:\/\//.test(value);
  return (
    <li>
      <strong>{name}</strong>:{' '}
      {isUrl && !long ? <SafeLink href={value}>{value}</SafeLink> : <span>{text}</span>}
      {long && (
        <button type="button" onClick={() => setOpen(!open)}>
          {open ? 'show less' : 'show more'}
        </button>
      )}
    </li>
  );
}

function Pairs({ title, pairs }: { title: string; pairs: Record<string, string> }) {
  const entries = Object.entries(pairs).sort(([a], [b]) => a.localeCompare(b));
  if (entries.length === 0) return null;
  return (
    <section aria-label={title}>
      <h3>{title}</h3>
      <ul>
        {entries.map(([k, v]) =>
          title === 'Annotations' ? (
            <Annotation key={k} name={k} value={v} />
          ) : (
            <li key={k}>{`${k}=${v}`}</li>
          ),
        )}
      </ul>
    </section>
  );
}

function stringMap(v: unknown): Record<string, string> {
  if (v === null || typeof v !== 'object') return {};
  return Object.fromEntries(Object.entries(v as Record<string, unknown>).map(([k, val]) => [k, String(val)]));
}

function quantities(o: K8sObject, path: string): [string, string][] {
  const m = get(o, path);
  if (m === null || typeof m !== 'object') return [];
  return Object.entries(m as Record<string, unknown>)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([k, v]) => [
      k,
      k === 'memory' || k === 'ephemeral-storage' ? formatBytes(parseQuantity(String(v))) : String(v),
    ]);
}

interface Props {
  cluster: string;
  group: string;
  kind: string;
  object: K8sObject;
  now: number;
  chain: ChainLink[];
  /** The live pods of the object's namespace, for Services, claims and workloads. */
  pods: K8sObject[];
}

function WorkloadSection({ object }: { object: K8sObject }) {
  const strategy = getString(object, 'spec.strategy.type') || getString(object, 'spec.updateStrategy.type');
  const desired = get(object, 'spec.replicas');
  return (
    <section aria-label="Workload">
      <h3>Workload</h3>
      <dl>
        {strategy && (
          <>
            <dt>Strategy</dt>
            <dd>{strategy}</dd>
          </>
        )}
        {typeof desired === 'number' && (
          <>
            <dt>Replicas</dt>
            <dd>{`${String(get(object, 'status.readyReplicas') ?? 0)} ready of ${desired}`}</dd>
          </>
        )}
        <dt>Selector</dt>
        <dd>{labelString(get(object, 'spec.selector.matchLabels'))}</dd>
      </dl>
    </section>
  );
}

function NodeSection({ object }: { object: K8sObject }) {
  const taints = getArray(object, 'spec.taints').map(
    (t) =>
      `${getString(t, 'key')}${getString(t, 'value') ? `=${getString(t, 'value')}` : ''}:${getString(t, 'effect')}`,
  );
  const pressure = getArray(object, 'status.conditions').filter(
    (c) => /Pressure$|NetworkUnavailable$/.test(getString(c, 'type')) && getString(c, 'status') === 'True',
  );
  return (
    <section aria-label="Node">
      <h3>Node</h3>
      <dl>
        <dt>Capacity</dt>
        <dd>
          {quantities(object, 'status.capacity')
            .map(([k, v]) => `${k}: ${v}`)
            .join(', ')}
        </dd>
        <dt>Allocatable</dt>
        <dd>
          {quantities(object, 'status.allocatable')
            .map(([k, v]) => `${k}: ${v}`)
            .join(', ')}
        </dd>
        <dt>Taints</dt>
        <dd>{taints.join(', ') || 'none'}</dd>
        <dt>Pressure</dt>
        <dd>{pressure.map((c) => getString(c, 'type')).join(', ') || 'none'}</dd>
      </dl>
    </section>
  );
}

function PodLinks({ cluster, label, pods }: { cluster: string; label: string; pods: K8sObject[] }) {
  return (
    <section aria-label={label}>
      <h3>{label}</h3>
      {pods.length === 0 ? (
        <p>None</p>
      ) : (
        <ul>
          {pods.map((p) => (
            <li key={getString(p, 'metadata.uid') || getString(p, 'metadata.name')}>
              <Link
                to={resourcePath({
                  cluster,
                  namespace: getString(p, 'metadata.namespace'),
                  group: '',
                  kind: 'Pod',
                  name: getString(p, 'metadata.name'),
                })}
              >
                {getString(p, 'metadata.name')}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function ServiceSection({
  cluster,
  object,
  pods,
}: {
  cluster: string;
  object: K8sObject;
  pods: K8sObject[];
}) {
  const ports = getArray(object, 'spec.ports').map(
    (p) =>
      `${getString(p, 'name') || 'port'}: ${String(get(p, 'port'))}→${String(get(p, 'targetPort') ?? '')}/${getString(p, 'protocol') || 'TCP'}`,
  );
  return (
    <>
      <section aria-label="Ports">
        <h3>Ports</h3>
        <ul>
          {ports.map((p) => (
            <li key={p}>{p}</li>
          ))}
        </ul>
      </section>
      <PodLinks cluster={cluster} label="Matched pods" pods={podsForService(object, pods)} />
    </>
  );
}

function IngressSection({ cluster, object }: { cluster: string; object: K8sObject }) {
  const ns = getString(object, 'metadata.namespace');
  const rules = getArray(object, 'spec.rules').flatMap((r) =>
    getArray(r, 'http.paths').map((p) => ({
      host: getString(r, 'host'),
      path: getString(p, 'path') || '/',
      service: getString(p, 'backend.service.name'),
    })),
  );
  return (
    <section aria-label="Rules">
      <h3>Rules</h3>
      <ul>
        {rules.map((r, i) => (
          <li key={`${r.host}${r.path}${i}`}>
            {`${r.host || '*'}${r.path} → `}
            {r.service ? (
              <Link
                to={resourcePath({ cluster, namespace: ns, group: '', kind: 'Service', name: r.service })}
              >
                {r.service}
              </Link>
            ) : (
              'no service'
            )}
          </li>
        ))}
      </ul>
      {servicesOfIngress(object).length === 0 && <p>No backends</p>}
    </section>
  );
}

function ClaimSection({ cluster, object, pods }: { cluster: string; object: K8sObject; pods: K8sObject[] }) {
  const volume = getString(object, 'spec.volumeName');
  return (
    <>
      <section aria-label="Volume">
        <h3>Volume</h3>
        {volume ? (
          <Link to={resourcePath({ cluster, group: '', kind: 'PersistentVolume', name: volume })}>
            {volume}
          </Link>
        ) : (
          <p>Not bound</p>
        )}
      </section>
      <PodLinks cluster={cluster} label="Mounted by" pods={podsMountingClaim(object, pods)} />
    </>
  );
}

/** What the object is: labels, annotations, conditions, owners and a section for its kind. */
export function OverviewTab({ cluster, group, kind, object, now, chain, pods }: Props) {
  const namespace = getString(object, 'metadata.namespace');
  const isCore = group === '' || group === 'core';
  return (
    <div>
      <OwnerChain chain={chain} cluster={cluster} namespace={namespace} />
      <Pairs title="Labels" pairs={stringMap(get(object, 'metadata.labels'))} />
      <Pairs title="Annotations" pairs={stringMap(get(object, 'metadata.annotations'))} />
      <Conditions conditions={getArray(object, 'status.conditions')} now={now} />
      {isCore && kind === 'Pod' && <ContainerList pod={object} />}
      {WORKLOADS.includes(kind) && groupOf(getString(object, 'apiVersion')) === 'apps' && (
        <WorkloadSection object={object} />
      )}
      {isCore && kind === 'Secret' && (
        <>
          <SecretData cluster={cluster} secret={object} />
          <CertificateDetails
            cluster={cluster}
            namespace={namespace}
            name={getString(object, 'metadata.name')}
            enabled={mayHoldCertificates(object)}
          />
        </>
      )}
      {isCore && kind === 'Node' && <NodeSection object={object} />}
      {isCore && kind === 'Service' && <ServiceSection cluster={cluster} object={object} pods={pods} />}
      {kind === 'Ingress' && <IngressSection cluster={cluster} object={object} />}
      {isCore && kind === 'PersistentVolumeClaim' && (
        <ClaimSection cluster={cluster} object={object} pods={pods} />
      )}
    </div>
  );
}
