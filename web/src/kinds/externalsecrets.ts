import { getArray, getString } from '../lib/path';
import { defineKind, pathColumn } from './columns';
import type { Column, KindDescriptor } from './types';

const readyReason: Column = {
  id: 'status',
  title: 'Status',
  value: (o) =>
    getString(
      getArray(o, 'status.conditions').find((c) => getString(c, 'type') === 'Ready'),
      'reason',
    ),
};

const provider: Column = {
  id: 'provider',
  title: 'Provider',
  value: (o) => {
    const p = (o.spec as { provider?: Record<string, unknown> } | undefined)?.provider;
    return p ? (Object.keys(p).sort()[0] ?? '') : '';
  },
};

const group = 'external-secrets.io';

/** The External Secrets kinds: ExternalSecret, SecretStore and ClusterSecretStore. */
export function externalSecretsDescriptors(): KindDescriptor[] {
  return [
    {
      ...defineKind({
        group,
        kind: 'ExternalSecret',
        resource: 'externalsecrets',
        namespaced: true,
        title: 'ExternalSecrets',
        section: 'Modules',
        columns: [
          pathColumn('store', 'Store', 'spec.secretStoreRef.name'),
          pathColumn('refresh', 'Refresh interval', 'spec.refreshInterval'),
          readyReason,
          {
            id: 'target',
            title: 'Target secret',
            value: (o) => getString(o, 'spec.target.name') || getString(o, 'metadata.name'),
          },
        ],
      }),
      actions: ['esoForceSync'],
    },
    defineKind({
      group,
      kind: 'SecretStore',
      resource: 'secretstores',
      namespaced: true,
      title: 'SecretStores',
      section: 'Modules',
      columns: [readyReason, provider],
    }),
    defineKind({
      group,
      kind: 'ClusterSecretStore',
      resource: 'clustersecretstores',
      namespaced: false,
      title: 'ClusterSecretStores',
      section: 'Modules',
      columns: [readyReason, provider],
    }),
  ];
}
