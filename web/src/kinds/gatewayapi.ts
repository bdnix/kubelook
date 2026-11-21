import { getArray, getString } from '../lib/path';
import { conditionStatus, defineKind, pathColumn } from './columns';
import type { Column, KindDescriptor } from './types';

const group = 'gateway.networking.k8s.io';
const cond = (id: string, title: string, type: string): Column => ({
  id,
  title,
  value: (o) => conditionStatus(type)(o),
});
const hostnames: Column = {
  id: 'hostnames',
  title: 'Hostnames',
  value: (o) => (getArray(o, 'spec.hostnames') as string[]).join(','),
};
const parents: Column = {
  id: 'parents',
  title: 'Parents',
  value: (o) =>
    getArray(o, 'spec.parentRefs')
      .map((p) => getString(p, 'name'))
      .filter((n) => n !== '')
      .join(','),
};

/** The Gateway API kinds the console has columns for; any other kind of the group gets the plain columns. */
export function gatewayDescriptors(): KindDescriptor[] {
  const base = { group, section: 'Modules' } as const;
  return [
    defineKind({
      ...base,
      kind: 'Gateway',
      resource: 'gateways',
      namespaced: true,
      title: 'Gateways',
      columns: [
        pathColumn('class', 'Class', 'spec.gatewayClassName'),
        cond('accepted', 'Accepted', 'Accepted'),
        cond('programmed', 'Programmed', 'Programmed'),
      ],
    }),
    defineKind({
      ...base,
      kind: 'GatewayClass',
      resource: 'gatewayclasses',
      namespaced: false,
      title: 'GatewayClasses',
      columns: [
        pathColumn('controller', 'Controller', 'spec.controllerName'),
        cond('accepted', 'Accepted', 'Accepted'),
      ],
    }),
    defineKind({
      ...base,
      kind: 'HTTPRoute',
      resource: 'httproutes',
      namespaced: true,
      title: 'HTTPRoutes',
      columns: [hostnames, parents],
    }),
    defineKind({
      ...base,
      kind: 'GRPCRoute',
      resource: 'grpcroutes',
      namespaced: true,
      title: 'GRPCRoutes',
      columns: [hostnames, parents],
    }),
    defineKind({
      ...base,
      kind: 'ReferenceGrant',
      resource: 'referencegrants',
      namespaced: true,
      title: 'ReferenceGrants',
      columns: [],
    }),
  ];
}
