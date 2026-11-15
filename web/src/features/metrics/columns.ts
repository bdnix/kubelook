import type { Column } from '../../kinds/types';
import { formatBytes, formatCores } from '../../lib/quantity';
import { getString } from '../../lib/path';
import type { MetricItem } from './useMetrics';

export const cpuText = (millicores: number): string => formatCores(millicores / 1000);
export const memoryText = (bytes: number): string => formatBytes(bytes);

/** The CPU and memory columns of a pods or nodes list, filled from the polled numbers. */
export function metricsColumns(byKey: Map<string, MetricItem>, kind: 'pod' | 'node'): Column[] {
  const lookup = (o: Record<string, unknown>) => {
    const name = getString(o, 'metadata.name');
    return byKey.get(kind === 'pod' ? `${getString(o, 'metadata.namespace')}/${name}` : name);
  };
  const suffix = kind === 'node' ? ' used' : '';
  return [
    {
      id: 'cpuUsage',
      title: `CPU${suffix}`,
      sort: 'number',
      value: (o) => lookup(o)?.cpuMillicores ?? '',
      text: (o) => {
        const m = lookup(o);
        return m ? cpuText(m.cpuMillicores) : '';
      },
    },
    {
      id: 'memoryUsage',
      title: `Memory${suffix}`,
      sort: 'number',
      value: (o) => lookup(o)?.memoryBytes ?? '',
      text: (o) => {
        const m = lookup(o);
        return m ? memoryText(m.memoryBytes) : '';
      },
    },
  ];
}
