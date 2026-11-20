import { describe, expect, it } from 'vitest';
import { ACTIONS } from './actions';
import { certManagerDescriptors } from './certmanager';
import { externalSecretsDescriptors } from './externalsecrets';
import { gatewayDescriptors } from './gatewayapi';
import { kedaDescriptors } from './keda';
import type { KindDescriptor } from './types';

const col = (d: KindDescriptor | undefined, id: string, o: unknown) =>
  d?.columns.find((c) => c.id === id)?.value(o as never, 0);
const titles = (d: KindDescriptor | undefined) => d?.columns.map((c) => c.title);
const cond = (type: string, status: string, extra: Record<string, unknown> = {}) => ({
  type,
  status,
  ...extra,
});

describe('KEDA', () => {
  const so = kedaDescriptors().find((d) => d.kind === 'ScaledObject');
  const o = {
    metadata: { name: 'q', namespace: 'ns' },
    spec: {
      scaleTargetRef: { kind: 'Deployment', name: 'worker' },
      minReplicaCount: 1,
      maxReplicaCount: 10,
      triggers: [
        { type: 'cpu', authenticationRef: { name: 'auth-a' } },
        { type: 'kafka' },
        { type: 'prometheus', authenticationRef: { name: 'auth-b' } },
      ],
    },
    status: {
      conditions: [
        cond('Ready', 'True'),
        cond('Active', 'False'),
        cond('Fallback', 'False'),
        cond('Paused', 'True'),
      ],
    },
  };
  it('has the columns of kubectl get so', () => {
    expect(titles(so)).toEqual([
      'Name',
      'Namespace',
      'ScaleTargetKind',
      'ScaleTargetName',
      'Min',
      'Max',
      'Ready',
      'Active',
      'Fallback',
      'Paused',
      'Triggers',
      'Authentications',
      'Age',
    ]);
    expect(col(so, 'targetkind', o)).toBe('Deployment');
    expect(col(so, 'targetname', o)).toBe('worker');
    expect(col(so, 'min', o)).toBe(1);
    expect(col(so, 'max', o)).toBe(10);
    expect(col(so, 'ready', o)).toBe('True');
    expect(col(so, 'active', o)).toBe('False');
    expect(col(so, 'paused', o)).toBe('True');
    expect(col(so, 'triggers', o)).toBe('cpu,kafka,prometheus');
    expect(col(so, 'authentications', o)).toBe('auth-a,auth-b');
  });
  it('offers pause and resume', () => {
    expect(so?.actions).toEqual(['kedaPause', 'kedaResume']);
    expect(ACTIONS.kedaPause?.label).toBe('Pause');
    expect(ACTIONS.kedaResume?.label).toBe('Resume');
    expect(
      ACTIONS.kedaPause?.access({ group: 'keda.sh', resource: 'scaledobjects', namespace: 'ns', name: 'q' }),
    ).toMatchObject({ verb: 'patch', group: 'keda.sh' });
  });
});

describe('External Secrets', () => {
  const ds = externalSecretsDescriptors();
  const es = ds.find((d) => d.kind === 'ExternalSecret');
  it('lists ExternalSecrets with store, refresh interval, status and the target secret', () => {
    expect(titles(es)).toEqual([
      'Name',
      'Namespace',
      'Store',
      'Refresh interval',
      'Status',
      'Target secret',
      'Age',
    ]);
    const o = {
      metadata: { name: 'db', namespace: 'ns' },
      spec: { secretStoreRef: { name: 'vault' }, refreshInterval: '1h', target: { name: 'db-secret' } },
      status: { conditions: [cond('Ready', 'True', { reason: 'SecretSynced' })] },
    };
    expect(col(es, 'store', o)).toBe('vault');
    expect(col(es, 'refresh', o)).toBe('1h');
    expect(col(es, 'status', o)).toBe('SecretSynced');
    expect(col(es, 'target', o)).toBe('db-secret');
    expect(col(es, 'target', { metadata: { name: 'db' }, spec: {} })).toBe('db');
    expect(es?.actions).toEqual(['esoForceSync']);
    expect(ACTIONS.esoForceSync?.label).toBe('Force sync');
  });
  it('lists stores with their status and provider', () => {
    const ss = ds.find((d) => d.kind === 'SecretStore');
    const css = ds.find((d) => d.kind === 'ClusterSecretStore');
    expect(titles(ss)).toEqual(['Name', 'Namespace', 'Status', 'Provider', 'Age']);
    expect(titles(css)).toEqual(['Name', 'Status', 'Provider', 'Age']);
    const o = {
      metadata: { name: 's' },
      spec: { provider: { vault: { server: 'x' }, aws: {} } },
      status: { conditions: [cond('Ready', 'True', { reason: 'Valid' })] },
    };
    expect(col(ss, 'provider', o)).toBe('aws');
    expect(col(css, 'status', o)).toBe('Valid');
  });
});

describe('cert-manager', () => {
  const ds = certManagerDescriptors();
  it('lists certificates with their expiry and renewal', () => {
    const c = ds.find((d) => d.kind === 'Certificate');
    expect(titles(c)).toEqual([
      'Name',
      'Namespace',
      'Ready',
      'Secret',
      'Issuer',
      'Not after',
      'Renewal',
      'Age',
    ]);
    const o = {
      metadata: { name: 'c', namespace: 'ns' },
      spec: { secretName: 'tls', issuerRef: { name: 'le' } },
      status: {
        notAfter: '2027-01-01T00:00:00Z',
        renewalTime: '2026-12-01T00:00:00Z',
        conditions: [cond('Ready', 'True')],
      },
    };
    expect(col(c, 'ready', o)).toBe('True');
    expect(col(c, 'secret', o)).toBe('tls');
    expect(col(c, 'issuer', o)).toBe('le');
    expect(col(c, 'notafter', o)).toBe('2027-01-01T00:00:00Z');
    expect(col(c, 'renewal', o)).toBe('2026-12-01T00:00:00Z');
    expect(c?.actions).toBeUndefined();
  });
  it('lists issuers and requests', () => {
    expect(titles(ds.find((d) => d.kind === 'Issuer'))).toEqual(['Name', 'Namespace', 'Ready', 'Age']);
    expect(titles(ds.find((d) => d.kind === 'ClusterIssuer'))).toEqual(['Name', 'Ready', 'Age']);
    const r = ds.find((d) => d.kind === 'CertificateRequest');
    expect(titles(r)).toEqual([
      'Name',
      'Namespace',
      'Approved',
      'Denied',
      'Ready',
      'Issuer',
      'Requestor',
      'Age',
    ]);
    const o = {
      spec: { issuerRef: { name: 'le' }, username: 'system:serviceaccount:a:b' },
      status: { conditions: [cond('Approved', 'True'), cond('Denied', 'False'), cond('Ready', 'True')] },
    };
    expect([
      col(r, 'approved', o),
      col(r, 'denied', o),
      col(r, 'ready', o),
      col(r, 'issuer', o),
      col(r, 'requestor', o),
    ]).toEqual(['True', 'False', 'True', 'le', 'system:serviceaccount:a:b']);
  });
});

describe('Gateway API', () => {
  const ds = gatewayDescriptors();
  it('adds hostnames, parents and class where the fields exist', () => {
    const route = ds.find((d) => d.kind === 'HTTPRoute');
    const o = {
      spec: { hostnames: ['a.example.com', 'b.example.com'], parentRefs: [{ name: 'gw1' }, { name: 'gw2' }] },
    };
    expect(col(route, 'hostnames', o)).toBe('a.example.com,b.example.com');
    expect(col(route, 'parents', o)).toBe('gw1,gw2');
    const gw = ds.find((d) => d.kind === 'Gateway');
    expect(col(gw, 'class', { spec: { gatewayClassName: 'nginx' } })).toBe('nginx');
    expect(ds.find((d) => d.kind === 'GatewayClass')?.namespaced).toBe(false);
    expect(ds.map((d) => d.kind)).toEqual(
      expect.arrayContaining(['Gateway', 'GatewayClass', 'HTTPRoute', 'GRPCRoute', 'ReferenceGrant']),
    );
  });
});
