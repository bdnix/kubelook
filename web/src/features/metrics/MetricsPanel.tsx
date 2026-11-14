import { getString } from '../../lib/path';
import { parseQuantity } from '../../lib/quantity';
import type { K8sObject } from '../../state/live';
import { cpuText, memoryText } from './columns';
import { Sparkline } from './Sparkline';
import { UsageBar } from './UsageBar';
import { useMetricsHistory, useNodeMetrics, usePodMetrics } from './useMetrics';

/** Usage bars of a node against what it can allocate. */
function NodeUsage({ cluster, object }: { cluster: string; object: K8sObject }) {
  const metrics = useNodeMetrics(cluster);
  const item = metrics.byKey.get(getString(object, 'metadata.name'));
  if (!metrics.data?.available || !item) return null;
  const cpu = parseQuantity(getString(object, 'status.allocatable.cpu'));
  const memory = parseQuantity(getString(object, 'status.allocatable.memory'));
  return (
    <>
      <UsageBar
        label="CPU"
        used={item.cpuMillicores}
        total={Number.isFinite(cpu) ? cpu * 1000 : 0}
        format={cpuText}
      />
      <UsageBar
        label="Memory"
        used={item.memoryBytes}
        total={Number.isFinite(memory) ? memory : 0}
        format={memoryText}
      />
    </>
  );
}

function PodUsage({ cluster, object }: { cluster: string; object: K8sObject }) {
  const namespace = getString(object, 'metadata.namespace');
  const metrics = usePodMetrics(cluster, namespace);
  const item = metrics.byKey.get(`${namespace}/${getString(object, 'metadata.name')}`);
  if (!metrics.data?.available || !item) return null;
  return (
    <p>
      CPU {cpuText(item.cpuMillicores)}, memory {memoryText(item.memoryBytes)}
    </p>
  );
}

function History({ cluster, kind, object }: { cluster: string; kind: 'pod' | 'node'; object: K8sObject }) {
  const history = useMetricsHistory(
    cluster,
    kind,
    getString(object, 'metadata.namespace'),
    getString(object, 'metadata.name'),
  );
  if (!history.data?.available) return null;
  return (
    <>
      {history.data.series.map((s) => (
        <div key={s.metric}>
          <span>{s.metric === 'cpu' ? 'CPU' : 'Memory'}</span>{' '}
          <Sparkline label={`${s.metric === 'cpu' ? 'CPU' : 'Memory'}, last 60 minutes`} points={s.points} />
        </div>
      ))}
    </>
  );
}

/** The usage of a pod or node in its details: current numbers from metrics-server, history from Prometheus. */
export function MetricsPanel({
  cluster,
  kind,
  object,
}: {
  cluster: string;
  kind: string;
  object: K8sObject;
}) {
  if (kind !== 'Pod' && kind !== 'Node') return null;
  return (
    <section aria-label="Usage">
      {kind === 'Node' ? (
        <NodeUsage cluster={cluster} object={object} />
      ) : (
        <PodUsage cluster={cluster} object={object} />
      )}
      <History cluster={cluster} kind={kind === 'Node' ? 'node' : 'pod'} object={object} />
    </section>
  );
}
