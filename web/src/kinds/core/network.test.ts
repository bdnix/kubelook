import { describe, expect, it } from 'vitest';
import { expectToleratesMissingFields, meta, row } from '../testing';
import {
  endpointSlicesKind,
  endpointsKind,
  ingressClassesKind,
  ingressesKind,
  networkPoliciesKind,
  servicesKind,
} from './network';

describe('network kinds', () => {
  it('Service shows type, IPs and ports with node ports and protocols', () => {
    const svc = {
      metadata: meta('web', 'ns'),
      spec: {
        type: 'LoadBalancer',
        clusterIP: '10.0.0.1',
        externalIPs: ['1.2.3.4'],
        ports: [{ port: 80, nodePort: 30080, protocol: 'TCP' }, { port: 53, protocol: 'UDP' }, { port: 443 }],
      },
      status: { loadBalancer: { ingress: [{ ip: '5.6.7.8' }, { hostname: 'lb.example' }, {}] } },
    };
    expect(row(servicesKind, svc)).toMatchObject({
      type: 'LoadBalancer',
      clusterIP: '10.0.0.1',
      externalIP: '1.2.3.4,5.6.7.8,lb.example',
      ports: '80:30080/TCP,53/UDP,443/TCP',
    });
    expectToleratesMissingFields(servicesKind);
  });

  it('Endpoints lists addresses with ports, shortened after three, and carries the deprecation note', () => {
    const ep = (n: number, ports: unknown[]) => ({
      metadata: meta('web', 'ns'),
      subsets: [{ addresses: Array.from({ length: n }, (_, i) => ({ ip: `10.0.0.${i + 1}` })), ports }],
    });
    expect(row(endpointsKind, ep(2, [{ port: 8080 }])).endpoints).toBe('10.0.0.1:8080,10.0.0.2:8080');
    expect(row(endpointsKind, ep(1, [])).endpoints).toBe('10.0.0.1');
    expect(row(endpointsKind, ep(5, [{ port: 80 }])).endpoints).toBe(
      '10.0.0.1:80,10.0.0.2:80,10.0.0.3:80 + 2 more...',
    );
    expect(endpointsKind.note).toBe(
      'Endpoints is deprecated since Kubernetes 1.33 in favour of EndpointSlices.',
    );
    expectToleratesMissingFields(endpointsKind);
  });

  it('EndpointSlice shows address type, ports, endpoints and the service label', () => {
    const slice = {
      metadata: meta('web-x', 'ns', { labels: { 'kubernetes.io/service-name': 'web' } }),
      addressType: 'IPv4',
      ports: [{ port: 80 }, { port: 443 }],
      endpoints: [{ addresses: ['10.0.0.1'] }, { addresses: ['10.0.0.2'] }],
    };
    expect(row(endpointSlicesKind, slice)).toMatchObject({
      addressType: 'IPv4',
      ports: '80,443',
      endpoints: '10.0.0.1,10.0.0.2',
      service: 'web',
    });
    expectToleratesMissingFields(endpointSlicesKind);
  });

  it('Ingress shows class (field or annotation), hosts, address and ports', () => {
    const ing = {
      metadata: meta('i', 'ns'),
      spec: {
        ingressClassName: 'nginx',
        rules: [{ host: 'a.example' }, {}, { host: 'b.example' }],
        tls: [{}],
      },
      status: { loadBalancer: { ingress: [{ ip: '9.9.9.9' }] } },
    };
    expect(row(ingressesKind, ing)).toMatchObject({
      class: 'nginx',
      hosts: 'a.example,b.example',
      address: '9.9.9.9',
      ports: '80, 443',
    });
    const legacy = {
      metadata: meta('i', 'ns', { annotations: { 'kubernetes.io/ingress.class': 'traefik' } }),
      spec: {},
    };
    expect(row(ingressesKind, legacy)).toMatchObject({ class: 'traefik', ports: '80' });
    expectToleratesMissingFields(ingressesKind);
  });

  it('IngressClass shows the controller and whether it is the default', () => {
    const ic = (annotations: object) => ({
      metadata: meta('nginx', undefined, { annotations }),
      spec: { controller: 'k8s.io/ingress-nginx' },
    });
    expect(
      row(ingressClassesKind, ic({ 'ingressclass.kubernetes.io/is-default-class': 'true' })),
    ).toMatchObject({ controller: 'k8s.io/ingress-nginx', default: 'true' });
    expect(row(ingressClassesKind, ic({})).default).toBe('false');
    expectToleratesMissingFields(ingressClassesKind);
  });

  it('NetworkPolicy shows the pod selector and policy types', () => {
    const np = {
      metadata: meta('n', 'ns'),
      spec: { podSelector: { matchLabels: { app: 'web' } }, policyTypes: ['Ingress', 'Egress'] },
    };
    expect(row(networkPoliciesKind, np)).toMatchObject({
      podSelector: 'app=web',
      policyTypes: 'Ingress,Egress',
    });
    expect(
      row(networkPoliciesKind, { metadata: meta('n', 'ns'), spec: { podSelector: {} } }).podSelector,
    ).toBe('(all pods)');
    expectToleratesMissingFields(networkPoliciesKind);
  });
});
