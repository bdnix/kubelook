import { describe, expect, it } from 'vitest';
import { trivyDescriptors } from './trivy';

describe('trivyDescriptors', () => {
  const ds = trivyDescriptors();
  const find = (kind: string) => ds.find((d) => d.kind === kind);
  const report = {
    metadata: {
      name: 'r',
      namespace: 'ns',
      labels: { 'trivy-operator.resource.kind': 'Deployment', 'trivy-operator.resource.name': 'web' },
    },
    report: { summary: { criticalCount: 2, highCount: 5, mediumCount: 7, lowCount: 11 } },
  };
  const value = (kind: string, id: string) =>
    find(kind)
      ?.columns.find((c) => c.id === id)
      ?.value(report as never, 0);

  it('has a descriptor for every report kind, namespaced except the cluster kinds', () => {
    expect(ds).toHaveLength(13);
    expect(find('VulnerabilityReport')?.namespaced).toBe(true);
    for (const k of ['ClusterVulnerabilityReport', 'ClusterComplianceReport', 'ClusterExposedSecretReport'])
      expect(find(k)?.namespaced).toBe(false);
    for (const d of ds) expect(d.section).toBe('Modules');
  });

  it('shows the resource and the severity counts of the summary', () => {
    expect(find('VulnerabilityReport')?.columns.map((c) => c.title)).toEqual([
      'Name',
      'Namespace',
      'Resource',
      'Critical',
      'High',
      'Medium',
      'Low',
      'Age',
    ]);
    expect(value('VulnerabilityReport', 'resource')).toBe('Deployment/web');
    expect(value('VulnerabilityReport', 'critical')).toBe(2);
    expect(value('VulnerabilityReport', 'high')).toBe(5);
    expect(value('VulnerabilityReport', 'medium')).toBe(7);
    expect(value('VulnerabilityReport', 'low')).toBe(11);
  });

  it('marks a report with critical findings as an error and a clean one as ok', () => {
    const d = find('VulnerabilityReport');
    expect(d?.status?.(report).severity).toBe('error');
    expect(d?.status?.({ report: { summary: { criticalCount: 0, highCount: 1 } } }).severity).toBe('warning');
    expect(d?.status?.({ report: { summary: { criticalCount: 0, highCount: 0 } } }).severity).toBe('ok');
  });
});
