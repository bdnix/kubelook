import { conditionStatus } from '../../kinds/columns';
import { getArray, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';

const FAILING_REASONS = [
  'CrashLoopBackOff',
  'ImagePullBackOff',
  'ErrImagePull',
  'CreateContainerConfigError',
];

export interface PodRef {
  name: string;
  namespace: string;
}

export interface FailingPod extends PodRef {
  reason: string;
}

export interface PendingPod extends PodRef {
  message: string;
}

export interface NotReadyPod extends PodRef {
  message: string;
  containers: string[];
}

const ref = (p: K8sObject): PodRef => ({
  name: getString(p, 'metadata.name'),
  namespace: getString(p, 'metadata.namespace'),
});

function waitingReason(p: K8sObject): string {
  for (const path of ['status.containerStatuses', 'status.initContainerStatuses']) {
    for (const c of getArray(p, path)) {
      const reason = getString(c, 'state.waiting.reason');
      if (FAILING_REASONS.includes(reason)) return reason;
    }
  }
  return '';
}

/** Pods that failed, or whose containers wait with CrashLoopBackOff, ImagePullBackOff, ErrImagePull or CreateContainerConfigError. */
export function failingPods(pods: K8sObject[]): FailingPod[] {
  const out: FailingPod[] = [];
  for (const p of pods) {
    if (getString(p, 'status.phase') === 'Failed')
      out.push({ ...ref(p), reason: getString(p, 'status.reason') || 'Failed' });
    else {
      const reason = waitingReason(p);
      if (reason) out.push({ ...ref(p), reason });
    }
  }
  return out;
}

const when = (e: K8sObject) =>
  Date.parse(
    getString(e, 'lastTimestamp') || getString(e, 'eventTime') || getString(e, 'metadata.creationTimestamp'),
  ) || 0;
const subject = (e: K8sObject) =>
  (e.involvedObject ?? e.regarding) as { kind?: string; name?: string; namespace?: string } | undefined;

/** Pending pods, each with the message of its latest FailedScheduling event. */
export function pendingPods(pods: K8sObject[], events: K8sObject[]): PendingPod[] {
  const scheduling = events.filter((e) => getString(e, 'reason') === 'FailedScheduling');
  return pods
    .filter((p) => getString(p, 'status.phase') === 'Pending')
    .map((p) => {
      const r = ref(p);
      const latest = scheduling
        .filter((e) => subject(e)?.name === r.name && (subject(e)?.namespace ?? r.namespace) === r.namespace)
        .sort((a, b) => when(b) - when(a))[0];
      return { ...r, message: latest ? getString(latest, 'message') || getString(latest, 'note') : '' };
    });
}

/** Running pods that are not ready: the failing condition's message and the containers that are not ready. */
export function notReadyPods(pods: K8sObject[]): NotReadyPod[] {
  const out: NotReadyPod[] = [];
  for (const p of pods) {
    if (getString(p, 'status.phase') !== 'Running') continue;
    const conditions = getArray(p, 'status.conditions');
    const failing = ['Ready', 'ContainersReady']
      .map((t) => conditions.find((c) => getString(c, 'type') === t && getString(c, 'status') === 'False'))
      .find((c) => c !== undefined);
    if (!failing) continue;
    out.push({
      ...ref(p),
      message: getString(failing, 'message'),
      containers: getArray(p, 'status.containerStatuses')
        .filter((c) => (c as { ready?: unknown }).ready === false)
        .map((c) => getString(c, 'name')),
    });
  }
  return out;
}

export interface NodeCounts {
  total: number;
  ready: number;
  notReady: number;
  cordoned: number;
  underPressure: number;
}

const PRESSURE = ['MemoryPressure', 'DiskPressure', 'PIDPressure', 'NetworkUnavailable'];

export function nodeHealth(nodes: K8sObject[]): NodeCounts {
  const c: NodeCounts = { total: nodes.length, ready: 0, notReady: 0, cordoned: 0, underPressure: 0 };
  for (const n of nodes) {
    if (conditionStatus('Ready')(n) === 'True') c.ready++;
    else c.notReady++;
    if ((n.spec as { unschedulable?: unknown } | undefined)?.unschedulable === true) c.cordoned++;
    if (PRESSURE.some((t) => conditionStatus(t)(n) === 'True')) c.underPressure++;
  }
  return c;
}

const BUCKET_MS = 5 * 60_000;

export interface Bucket {
  start: number;
  count: number;
}

/** Warning events per five minutes, from the oldest the cluster holds up to `now`, with empty buckets in between. */
export function warningTimeline(events: K8sObject[], now: number): Bucket[] {
  const times = events
    .filter((e) => getString(e, 'type') === 'Warning')
    .map(when)
    .filter((t) => t > 0);
  if (times.length === 0) return [];
  const first = Math.floor(Math.min(...times) / BUCKET_MS) * BUCKET_MS;
  const last = Math.floor(now / BUCKET_MS) * BUCKET_MS;
  const out: Bucket[] = [];
  for (let start = first; start <= last; start += BUCKET_MS) out.push({ start, count: 0 });
  for (const t of times) {
    const b = out[Math.floor((Math.floor(t / BUCKET_MS) * BUCKET_MS - first) / BUCKET_MS)];
    if (b) b.count++;
  }
  return out;
}

/** Flux objects whose Ready condition is False. */
export function failedFlux(objects: K8sObject[]): number {
  return objects.filter((o) => conditionStatus('Ready')(o) === 'False').length;
}

/** Helm releases that failed or are in a pending state. */
export function failedHelm(releases: { status: string }[]): number {
  return releases.filter((r) => r.status === 'failed' || r.status.startsWith('pending-')).length;
}
