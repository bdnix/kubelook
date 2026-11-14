import { describe, expect, it } from 'vitest';
import { getString } from './path';
import {
  childrenOf,
  eventsFor,
  podsForService,
  podsMountingClaim,
  podsOfWorkload,
  servicesOfIngress,
} from './related';

const pod = (name: string, ns: string, labels: Record<string, string> = {}, spec: object = {}) => ({
  metadata: { name, namespace: ns, labels },
  spec,
});

describe('related objects', () => {
  it('childrenOf finds objects owned by the uid', () => {
    const rs = { metadata: { name: 'rs', ownerReferences: [{ uid: 'd1' }] } };
    const other = { metadata: { name: 'x', ownerReferences: [{ uid: 'd2' }] } };
    expect(childrenOf('d1', [rs, other, { metadata: {} }])).toEqual([rs]);
    expect(childrenOf('', [rs])).toEqual([]);
  });

  it('podsForService matches the selector within the namespace; no selector, no pods', () => {
    const svc = { metadata: { namespace: 'a' }, spec: { selector: { app: 'web' } } };
    const pods = [
      pod('p1', 'a', { app: 'web' }),
      pod('p2', 'b', { app: 'web' }),
      pod('p3', 'a', { app: 'db' }),
    ];
    expect(podsForService(svc, pods).map((p) => getString(p, 'metadata.name'))).toEqual(['p1']);
    expect(podsForService({ metadata: { namespace: 'a' }, spec: {} }, pods)).toEqual([]);
    expect(podsForService({ metadata: { namespace: 'a' }, spec: { selector: {} } }, pods)).toEqual([]);
  });

  it('servicesOfIngress lists the backends once, sorted', () => {
    const ing = {
      spec: {
        defaultBackend: { service: { name: 'fallback' } },
        rules: [
          {
            http: {
              paths: [
                { backend: { service: { name: 'web' } } },
                { backend: { service: { name: 'api' } } },
                { backend: {} },
              ],
            },
          },
          { http: { paths: [{ backend: { service: { name: 'web' } } }] } },
          {},
        ],
      },
    };
    expect(servicesOfIngress(ing)).toEqual(['api', 'fallback', 'web']);
    expect(servicesOfIngress({})).toEqual([]);
  });

  it('podsMountingClaim finds pods that use the claim in its namespace', () => {
    const claim = { metadata: { name: 'data', namespace: 'a' } };
    const mounts = { volumes: [{ persistentVolumeClaim: { claimName: 'data' } }] };
    const pods = [
      pod('p1', 'a', {}, mounts),
      pod('p2', 'b', {}, mounts),
      pod('p3', 'a', {}, { volumes: [{}] }),
    ];
    expect(podsMountingClaim(claim, pods).map((p) => getString(p, 'metadata.name'))).toEqual(['p1']);
  });

  it('podsOfWorkload uses the workload selector', () => {
    const dep = { metadata: { namespace: 'a' }, spec: { selector: { matchLabels: { app: 'web' } } } };
    const pods = [
      pod('p1', 'a', { app: 'web' }),
      pod('p2', 'a', { app: 'db' }),
      pod('p3', 'b', { app: 'web' }),
    ];
    expect(podsOfWorkload(dep, pods).map((p) => getString(p, 'metadata.name'))).toEqual(['p1']);
    expect(podsOfWorkload({ metadata: {}, spec: {} }, pods)).toEqual([]);
  });

  it('eventsFor filters by regarding or involvedObject uid, newest first', () => {
    const events = [
      { metadata: { name: 'old' }, regarding: { uid: 'u1' }, eventTime: '2026-11-14T10:00:00Z' },
      { metadata: { name: 'new' }, involvedObject: { uid: 'u1' }, lastTimestamp: '2026-11-14T11:00:00Z' },
      { metadata: { name: 'other' }, regarding: { uid: 'u2' } },
      {
        metadata: { name: 'series' },
        regarding: { uid: 'u1' },
        series: { lastObservedTime: '2026-11-14T12:00:00Z' },
      },
      { metadata: { name: 'created', creationTimestamp: '2026-11-14T09:00:00Z' }, regarding: { uid: 'u1' } },
      { metadata: { name: 'timeless' }, regarding: { uid: 'u1' } },
    ];
    expect(eventsFor('u1', events).map((e) => getString(e, 'metadata.name'))).toEqual([
      'series',
      'new',
      'old',
      'created',
      'timeless',
    ]);
    expect(eventsFor('', events)).toEqual([]);
  });
});
