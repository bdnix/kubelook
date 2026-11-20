import { getString } from '../lib/path';
import { defineKind, numberColumn } from './columns';
import type { KindDescriptor, Status } from './types';
import type { K8sObject } from '../state/live';

const KINDS: [kind: string, resource: string][] = [
  ['VulnerabilityReport', 'vulnerabilityreports'],
  ['ConfigAuditReport', 'configauditreports'],
  ['ExposedSecretReport', 'exposedsecretreports'],
  ['RbacAssessmentReport', 'rbacassessmentreports'],
  ['InfraAssessmentReport', 'infraassessmentreports'],
  ['SbomReport', 'sbomreports'],
  ['ClusterVulnerabilityReport', 'clustervulnerabilityreports'],
  ['ClusterConfigAuditReport', 'clusterconfigauditreports'],
  ['ClusterRbacAssessmentReport', 'clusterrbacassessmentreports'],
  ['ClusterInfraAssessmentReport', 'clusterinfraassessmentreports'],
  ['ClusterSbomReport', 'clustersbomreports'],
  ['ClusterExposedSecretReport', 'clusterexposedsecretreports'],
  ['ClusterComplianceReport', 'clustercompliancereports'],
];

const label = (o: K8sObject, key: string) =>
  ((o.metadata as { labels?: Record<string, string> } | undefined)?.labels ?? {})[key] ?? '';

function status(o: K8sObject): Status {
  const count = (k: string) =>
    Number((o.report as { summary?: Record<string, number> } | undefined)?.summary?.[k] ?? 0);
  if (count('criticalCount') > 0) return { text: 'Critical findings', severity: 'error' };
  if (count('highCount') > 0) return { text: 'High findings', severity: 'warning' };
  return { text: 'Clean', severity: 'ok' };
}

/** The Trivy Operator report kinds, with the severity counts of `report.summary`. */
export function trivyDescriptors(): KindDescriptor[] {
  return KINDS.map(([kind, resource]) =>
    defineKind({
      group: 'aquasecurity.github.io',
      kind,
      resource,
      namespaced: !kind.startsWith('Cluster'),
      title: kind,
      section: 'Modules',
      columns: [
        {
          id: 'resource',
          title: 'Resource',
          value: (o) => {
            const k = label(o, 'trivy-operator.resource.kind');
            const n = label(o, 'trivy-operator.resource.name');
            return k && n ? `${k}/${n}` : getString(o, 'metadata.name');
          },
        },
        numberColumn('critical', 'Critical', 'report.summary.criticalCount'),
        numberColumn('high', 'High', 'report.summary.highCount'),
        numberColumn('medium', 'Medium', 'report.summary.mediumCount'),
        numberColumn('low', 'Low', 'report.summary.lowCount'),
      ],
      status,
    }),
  );
}
