import { get, getArray, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { annotationValue, defineKind, labelString, labelValue, pathColumn } from '../columns';
import type { KindDescriptor } from '../types';

function ingressAddresses(o: K8sObject): string[] {
  return getArray(o, 'status.loadBalancer.ingress')
    .map((i) => getString(i, 'ip') || getString(i, 'hostname'))
    .filter((a) => a !== '');
}

function servicePorts(o: K8sObject): string {
  return getArray(o, 'spec.ports')
    .map((p) => {
      const port = get(p, 'port');
      const nodePort = get(p, 'nodePort');
      const protocol = getString(p, 'protocol') || 'TCP';
      return `${String(port)}${typeof nodePort === 'number' ? `:${nodePort}` : ''}/${protocol}`;
    })
    .join(',');
}

function externalIP(o: K8sObject): string {
  const external = getArray(o, 'spec.externalIPs').filter((x): x is string => typeof x === 'string');
  return [...external, ...ingressAddresses(o)].join(',');
}

const MAX_ADDRESSES = 3;

function endpointAddresses(o: K8sObject): string {
  const out: string[] = [];
  for (const subset of getArray(o, 'subsets')) {
    const ports = getArray(subset, 'ports').map((p) => String(get(p, 'port')));
    for (const a of getArray(subset, 'addresses')) {
      const ip = getString(a, 'ip');
      if (ports.length === 0) out.push(ip);
      for (const p of ports) out.push(`${ip}:${p}`);
    }
  }
  if (out.length <= MAX_ADDRESSES) return out.join(',');
  return `${out.slice(0, MAX_ADDRESSES).join(',')} + ${out.length - MAX_ADDRESSES} more...`;
}

function sliceEndpoints(o: K8sObject): string {
  return getArray(o, 'endpoints')
    .flatMap((e) => getArray(e, 'addresses'))
    .filter((a): a is string => typeof a === 'string')
    .join(',');
}

function slicePorts(o: K8sObject): string {
  return getArray(o, 'ports')
    .map((p) => String(get(p, 'port')))
    .join(',');
}

function ingressPorts(o: K8sObject): string {
  return getArray(o, 'spec.tls').length > 0 ? '80, 443' : '80';
}

function selector(o: K8sObject): string {
  const s = labelString(get(o, 'spec.podSelector.matchLabels'));
  return s || '(all pods)';
}

export const servicesKind: KindDescriptor = defineKind({
  group: '',
  kind: 'Service',
  resource: 'services',
  namespaced: true,
  title: 'Services',
  section: 'Network',
  actions: ['delete'],
  columns: [
    pathColumn('type', 'Type', 'spec.type'),
    pathColumn('clusterIP', 'Cluster IP', 'spec.clusterIP'),
    { id: 'externalIP', title: 'External IP', value: externalIP },
    { id: 'ports', title: 'Ports', value: servicePorts },
  ],
});

export const endpointsKind: KindDescriptor = {
  ...defineKind({
    group: '',
    kind: 'Endpoints',
    resource: 'endpoints',
    namespaced: true,
    title: 'Endpoints',
    section: 'Network',
    actions: ['delete'],
    columns: [{ id: 'endpoints', title: 'Endpoints', value: endpointAddresses }],
  }),
  note: 'Endpoints is deprecated since Kubernetes 1.33 in favour of EndpointSlices.',
};

export const endpointSlicesKind: KindDescriptor = defineKind({
  group: 'discovery.k8s.io',
  kind: 'EndpointSlice',
  resource: 'endpointslices',
  namespaced: true,
  title: 'EndpointSlices',
  section: 'Network',
  actions: ['delete'],
  columns: [
    pathColumn('addressType', 'Address type', 'addressType'),
    { id: 'ports', title: 'Ports', value: slicePorts },
    { id: 'endpoints', title: 'Endpoints', value: sliceEndpoints },
    { id: 'service', title: 'Service', value: (o) => labelValue(o, 'kubernetes.io/service-name') },
  ],
});

export const ingressesKind: KindDescriptor = defineKind({
  group: 'networking.k8s.io',
  kind: 'Ingress',
  resource: 'ingresses',
  namespaced: true,
  title: 'Ingresses',
  section: 'Network',
  actions: ['delete'],
  columns: [
    {
      id: 'class',
      title: 'Class',
      value: (o) =>
        getString(o, 'spec.ingressClassName') || annotationValue(o, 'kubernetes.io/ingress.class'),
    },
    {
      id: 'hosts',
      title: 'Hosts',
      value: (o) =>
        getArray(o, 'spec.rules')
          .map((r) => getString(r, 'host'))
          .filter((h) => h !== '')
          .join(','),
    },
    { id: 'address', title: 'Address', value: (o) => ingressAddresses(o).join(',') },
    { id: 'ports', title: 'Ports', value: ingressPorts },
  ],
});

export const ingressClassesKind: KindDescriptor = defineKind({
  group: 'networking.k8s.io',
  kind: 'IngressClass',
  resource: 'ingressclasses',
  namespaced: false,
  title: 'IngressClasses',
  section: 'Network',
  actions: ['delete'],
  columns: [
    pathColumn('controller', 'Controller', 'spec.controller'),
    {
      id: 'default',
      title: 'Default',
      value: (o) =>
        annotationValue(o, 'ingressclass.kubernetes.io/is-default-class') === 'true' ? 'true' : 'false',
    },
  ],
});

export const networkPoliciesKind: KindDescriptor = defineKind({
  group: 'networking.k8s.io',
  kind: 'NetworkPolicy',
  resource: 'networkpolicies',
  namespaced: true,
  title: 'NetworkPolicies',
  section: 'Network',
  actions: ['delete'],
  columns: [
    { id: 'podSelector', title: 'Pod selector', value: selector },
    {
      id: 'policyTypes',
      title: 'Policy types',
      value: (o) =>
        getArray(o, 'spec.policyTypes')
          .filter((t): t is string => typeof t === 'string')
          .join(','),
    },
  ],
});
