import { formatAge } from '../lib/age';
import { getString } from '../lib/path';
import * as access from './core/access';
import * as cluster from './core/cluster';
import * as config from './core/config';
import * as network from './core/network';
import { podsKind } from './core/pods';
import * as storage from './core/storage';
import * as workloads from './core/workloads';
import type { KindDescriptor } from './types';

const isKind = (v: unknown): v is KindDescriptor => typeof v === 'object' && v !== null && 'columns' in v;

const KINDS: KindDescriptor[] = [
  podsKind,
  ...[workloads, config, network, storage, cluster, access].flatMap((m) => Object.values(m).filter(isKind)),
];

/** The descriptor of a kind; kinds without one get Name, Namespace and Age. */
export function getKind(group: string, kind: string): KindDescriptor {
  const g = group === 'core' ? '' : group;
  const found = KINDS.find((k) => k.group === g && k.kind === kind);
  if (found) return found;
  return {
    group: g,
    kind,
    resource: kind.toLowerCase(),
    namespaced: true,
    title: kind,
    section: 'Other',
    columns: [
      { id: 'name', title: 'Name', value: (o) => getString(o, 'metadata.name') },
      { id: 'namespace', title: 'Namespace', value: (o) => getString(o, 'metadata.namespace') },
      {
        id: 'age',
        title: 'Age',
        value: (o, now) => formatAge(getString(o, 'metadata.creationTimestamp'), now),
        sort: 'age',
      },
    ],
  };
}

/** Every kind that has its own descriptor, for the sidebar. */
export function knownKinds(): KindDescriptor[] {
  return KINDS;
}
