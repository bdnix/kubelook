import { getArray, getString } from '../lib/path';
import { conditionStatus, defineKind, pathColumn } from './columns';
import type { Column, KindDescriptor, Status } from './types';

const created = (o: Parameters<ReturnType<typeof conditionStatus>>[0]) => conditionStatus('SecretCreated')(o);

const reason: Column = {
  id: 'reason',
  title: 'Reason',
  value: (o) =>
    getString(
      getArray(o, 'status.conditions').find((c) => getString(c, 'type') === 'SecretCreated'),
      'reason',
    ),
};

/** The `ricoberger.de` VaultSecret kind. HashiCorp's Vault Secrets Operator uses other CRDs. */
export function vaultSecretDescriptors(): KindDescriptor[] {
  return [
    defineKind({
      group: 'ricoberger.de',
      kind: 'VaultSecret',
      resource: 'vaultsecrets',
      namespaced: true,
      title: 'VaultSecrets',
      section: 'Modules',
      columns: [
        { id: 'condition', title: 'Condition', value: (o) => created(o) },
        reason,
        {
          id: 'paths',
          title: 'Path(s)',
          value: (o) => {
            const paths = (getArray(o, 'spec.paths') as string[]).filter((p) => typeof p === 'string');
            return paths.length > 0 ? paths.join(', ') : getString(o, 'spec.path');
          },
        },
        pathColumn('engine', 'Engine', 'spec.secretEngine'),
        {
          id: 'secret',
          title: 'Secret',
          value: (o) => getString(o, 'spec.name') || getString(o, 'metadata.name'),
        },
      ],
      status: (o): Status =>
        created(o) === 'True'
          ? { text: 'Created', severity: 'ok' }
          : created(o) === 'False'
            ? { text: 'Failed', severity: 'error' }
            : { text: 'Unknown', severity: 'unknown' },
    }),
  ];
}
