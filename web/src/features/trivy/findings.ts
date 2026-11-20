import { getArray, getNumber, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';

export interface Finding {
  id: string;
  subject: string;
  detail: string;
  severity: string;
  title: string;
}

const CHECKS = [
  'ConfigAuditReport',
  'RbacAssessmentReport',
  'InfraAssessmentReport',
  'ClusterConfigAuditReport',
  'ClusterRbacAssessmentReport',
  'ClusterInfraAssessmentReport',
];

/** The findings of a report, as rows. Exposed-secret rows never carry the matched text. */
export function findingsOf(kind: string, o: K8sObject): Finding[] {
  if (kind.endsWith('VulnerabilityReport')) {
    return getArray(o, 'report.vulnerabilities').map((v) => {
      const installed = getString(v, 'installedVersion');
      const fixed = getString(v, 'fixedVersion');
      return {
        id: getString(v, 'vulnerabilityID'),
        subject: getString(v, 'resource'),
        detail: fixed ? `${installed} → ${fixed}` : installed,
        severity: getString(v, 'severity'),
        title: getString(v, 'title'),
      };
    });
  }
  if (CHECKS.includes(kind)) {
    return getArray(o, 'report.checks')
      .filter((c) => (c as { success?: unknown }).success !== true)
      .map((c) => ({
        id: getString(c, 'checkID'),
        subject: '',
        detail: (getArray(c, 'messages') as string[]).join('; '),
        severity: getString(c, 'severity'),
        title: getString(c, 'title'),
      }));
  }
  if (kind.endsWith('ExposedSecretReport')) {
    return getArray(o, 'report.secrets').map((s) => ({
      id: getString(s, 'ruleID'),
      subject: getString(s, 'target'),
      detail: getString(s, 'category'),
      severity: getString(s, 'severity'),
      title: getString(s, 'title'),
    }));
  }
  if (kind.endsWith('SbomReport')) {
    return getArray(o, 'report.components.components').map((c) => ({
      id: getString(c, 'name'),
      subject: getString(c, 'type'),
      detail: getString(c, 'version'),
      severity: '',
      title: '',
    }));
  }
  if (kind === 'ClusterComplianceReport') {
    return getArray(o, 'status.detailReport.results').map((r) => ({
      id: getString(r, 'id'),
      subject: '',
      detail: `${getNumber(r, 'totalFail')} failed`,
      severity: getString(r, 'severity'),
      title: getString(r, 'name'),
    }));
  }
  return [];
}

export interface Totals {
  critical: number;
  high: number;
  medium: number;
  low: number;
}

const counts = (o: K8sObject): Totals => ({
  critical: getNumber(o, 'report.summary.criticalCount'),
  high: getNumber(o, 'report.summary.highCount'),
  medium: getNumber(o, 'report.summary.mediumCount'),
  low: getNumber(o, 'report.summary.lowCount'),
});

export function severityTotals(reports: K8sObject[]): Totals {
  return reports.reduce<Totals>(
    (t, r) => {
      const c = counts(r);
      return {
        critical: t.critical + c.critical,
        high: t.high + c.high,
        medium: t.medium + c.medium,
        low: t.low + c.low,
      };
    },
    { critical: 0, high: 0, medium: 0, low: 0 },
  );
}

const TOP = 20;

/** The reports with the most critical findings, then the most high ones. */
export function topReports<T extends K8sObject>(reports: T[]): T[] {
  return [...reports]
    .sort((a, b) => counts(b).critical - counts(a).critical || counts(b).high - counts(a).high)
    .slice(0, TOP);
}

const label = (o: K8sObject, key: string) =>
  ((o.metadata as { labels?: Record<string, string> } | undefined)?.labels ?? {})[key] ?? '';

/** The reports about a workload, by the labels the operator puts on them. */
export function reportsForWorkload<T extends K8sObject>(
  reports: T[],
  w: { kind: string; name: string; namespace: string },
): T[] {
  return reports.filter(
    (r) =>
      label(r, 'trivy-operator.resource.kind') === w.kind &&
      label(r, 'trivy-operator.resource.name') === w.name &&
      label(r, 'trivy-operator.resource.namespace') === w.namespace,
  );
}
