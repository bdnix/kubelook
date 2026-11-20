import { conditionStatus, defineKind, pathColumn } from './columns';
import type { Column, KindDescriptor } from './types';

const cond = (id: string, title: string, type: string): Column => ({
  id,
  title,
  value: (o) => conditionStatus(type)(o),
});
const issuer = pathColumn('issuer', 'Issuer', 'spec.issuerRef.name');
const group = 'cert-manager.io';

/** The cert-manager kinds. They are edited and deleted like any object; no module actions. */
export function certManagerDescriptors(): KindDescriptor[] {
  const base = { group, section: 'Modules' } as const;
  return [
    defineKind({
      ...base,
      kind: 'Certificate',
      resource: 'certificates',
      namespaced: true,
      title: 'Certificates',
      columns: [
        cond('ready', 'Ready', 'Ready'),
        pathColumn('secret', 'Secret', 'spec.secretName'),
        issuer,
        pathColumn('notafter', 'Not after', 'status.notAfter'),
        pathColumn('renewal', 'Renewal', 'status.renewalTime'),
      ],
    }),
    defineKind({
      ...base,
      kind: 'Issuer',
      resource: 'issuers',
      namespaced: true,
      title: 'Issuers',
      columns: [cond('ready', 'Ready', 'Ready')],
    }),
    defineKind({
      ...base,
      kind: 'ClusterIssuer',
      resource: 'clusterissuers',
      namespaced: false,
      title: 'ClusterIssuers',
      columns: [cond('ready', 'Ready', 'Ready')],
    }),
    defineKind({
      ...base,
      kind: 'CertificateRequest',
      resource: 'certificaterequests',
      namespaced: true,
      title: 'CertificateRequests',
      columns: [
        cond('approved', 'Approved', 'Approved'),
        cond('denied', 'Denied', 'Denied'),
        cond('ready', 'Ready', 'Ready'),
        issuer,
        pathColumn('requestor', 'Requestor', 'spec.username'),
      ],
    }),
  ];
}
