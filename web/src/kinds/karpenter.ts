import { conditionStatus, defineKind, numberColumn, pathColumn } from './columns';
import type { Column, KindDescriptor, Status } from './types';

const POOL_LABEL = 'karpenter.sh/nodepool';

const label =
  (key: string) =>
  (o: unknown): string => {
    const labels = (o as { metadata?: { labels?: Record<string, string> } }).metadata?.labels;
    return labels?.[key] ?? '';
  };

const ready: Column = { id: 'ready', title: 'Ready', value: (o) => conditionStatus('Ready')(o) };

/** Ready=True is healthy, Ready=False is not. */
export function readyStatus(o: Parameters<ReturnType<typeof conditionStatus>>[0]): Status {
  switch (conditionStatus('Ready')(o)) {
    case 'True':
      return { text: 'Ready', severity: 'ok' };
    case 'False':
      return { text: 'Not ready', severity: 'error' };
    default:
      return { text: 'Unknown', severity: 'unknown' };
  }
}

const base = { namespaced: false, section: 'Modules', status: readyStatus } as const;

const nodePool = defineKind({
  ...base,
  group: 'karpenter.sh',
  kind: 'NodePool',
  resource: 'nodepools',
  title: 'NodePools',
  columns: [
    pathColumn('nodeclass', 'NodeClass', 'spec.template.spec.nodeClassRef.name'),
    numberColumn('nodes', 'Nodes', 'status.nodes'),
    ready,
  ],
});

// `kubectl get -o wide` shows these columns after Age, so they are appended after it.
const withTail = (d: KindDescriptor, tail: Column[]): KindDescriptor => ({
  ...d,
  columns: [...d.columns, ...tail],
});

const nodeClaim = defineKind({
  ...base,
  group: 'karpenter.sh',
  kind: 'NodeClaim',
  resource: 'nodeclaims',
  title: 'NodeClaims',
  columns: [
    { id: 'type', title: 'Type', value: (o) => label('node.kubernetes.io/instance-type')(o) },
    { id: 'capacity', title: 'Capacity', value: (o) => label('karpenter.sh/capacity-type')(o) },
    { id: 'zone', title: 'Zone', value: (o) => label('topology.kubernetes.io/zone')(o) },
    pathColumn('node', 'Node', 'status.nodeName'),
    ready,
  ],
});

const nodeOverlay = defineKind({
  ...base,
  group: 'karpenter.sh',
  kind: 'NodeOverlay',
  resource: 'nodeoverlays',
  title: 'NodeOverlays',
  columns: [ready, numberColumn('weight', 'Weight', 'spec.weight')],
});

const nodeClass = (group: string, kind: string, resource: string) =>
  defineKind({ ...base, group, kind, resource, title: `${kind}es`, columns: [ready] });

export function karpenterDescriptors(): KindDescriptor[] {
  const pool = withTail(nodePool, [
    numberColumn('weight', 'Weight', 'spec.weight'),
    pathColumn('cpu', 'CPU', 'status.resources.cpu'),
    pathColumn('memory', 'Memory', 'status.resources.memory'),
  ]);
  const claim = withTail(nodeClaim, [
    pathColumn('id', 'ID', 'status.providerID'),
    { id: 'nodepool', title: 'NodePool', value: (o) => label(POOL_LABEL)(o) },
    pathColumn('nodeclass', 'NodeClass', 'spec.nodeClassRef.name'),
    { id: 'drifted', title: 'Drifted', value: (o) => conditionStatus('Drifted')(o) },
  ]);
  return [
    pool,
    claim,
    nodeOverlay,
    nodeClass('karpenter.k8s.aws', 'EC2NodeClass', 'ec2nodeclasses'),
    nodeClass('karpenter.azure.com', 'AKSNodeClass', 'aksnodeclasses'),
    nodeClass('karpenter.k8s.gcp', 'GCENodeClass', 'gcenodeclasses'),
  ];
}
