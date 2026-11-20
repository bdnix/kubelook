import { describe, expect, it } from 'vitest';
import { findingsOf, reportsForWorkload, severityTotals, topReports } from './findings';

describe('findingsOf', () => {
  it('reads vulnerabilities', () => {
    const f = findingsOf('VulnerabilityReport', { report: { vulnerabilities: [{ vulnerabilityID: 'CVE-1', resource: 'openssl', installedVersion: '1.0', fixedVersion: '1.1', severity: 'CRITICAL', title: 'bad' }] } });
    expect(f).toEqual([{ id: 'CVE-1', subject: 'openssl', detail: '1.0 → 1.1', severity: 'CRITICAL', title: 'bad' }]);
  });
  it('reads checks of config, RBAC and infra assessments, failed ones only', () => {
    for (const kind of ['ConfigAuditReport', 'RbacAssessmentReport', 'InfraAssessmentReport', 'ClusterConfigAuditReport']) {
      const f = findingsOf(kind, { report: { checks: [{ checkID: 'KSV001', severity: 'HIGH', title: 't', messages: ['m1', 'm2'], success: false }, { checkID: 'KSV002', severity: 'LOW', success: true }] } });
      expect(f).toEqual([{ id: 'KSV001', subject: '', detail: 'm1; m2', severity: 'HIGH', title: 't' }]);
    }
  });
  it('reads exposed secrets without any matched text', () => {
    const f = findingsOf('ExposedSecretReport', { report: { secrets: [{ ruleID: 'aws', category: 'AWS', severity: 'CRITICAL', title: 'AWS key', target: '/a/.env', match: 'AKIA…' }] } });
    expect(f).toEqual([{ id: 'aws', subject: '/a/.env', detail: 'AWS', severity: 'CRITICAL', title: 'AWS key' }]);
    expect(JSON.stringify(f)).not.toContain('AKIA');
  });
  it('reads the components of an SBOM', () => {
    const f = findingsOf('SbomReport', { report: { components: { components: [{ name: 'lib', version: '2.0', type: 'library' }] } } });
    expect(f).toEqual([{ id: 'lib', subject: 'library', detail: '2.0', severity: '', title: '' }]);
  });
  it('reads the control results of a compliance report', () => {
    const f = findingsOf('ClusterComplianceReport', { status: { detailReport: { results: [{ id: '1.1', name: 'No root', severity: 'HIGH', totalFail: 3 }] } } });
    expect(f).toEqual([{ id: '1.1', subject: '', detail: '3 failed', severity: 'HIGH', title: 'No root' }]);
  });
  it('is empty without findings', () => {
    expect(findingsOf('VulnerabilityReport', {})).toEqual([]);
    expect(findingsOf('Unknown', { report: {} })).toEqual([]);
  });
});

const rep = (name: string, c: number, h: number) => ({ metadata: { name }, report: { summary: { criticalCount: c, highCount: h, mediumCount: 1, lowCount: 1 } } });

describe('topReports and severityTotals', () => {
  const reports = Array.from({ length: 25 }, (_, i) => rep(`r${i}`, i % 5, i));
  it('takes the 20 worst, by critical and then high', () => {
    const top = topReports(reports);
    expect(top).toHaveLength(20);
    expect(top[0]?.metadata).toEqual({ name: 'r24' });
    expect(top[1]?.metadata).toEqual({ name: 'r19' });
    expect(top.every((r, i) => i === 0 || ((top[i - 1] as typeof r).report.summary.criticalCount > r.report.summary.criticalCount || (top[i - 1] as typeof r).report.summary.highCount >= r.report.summary.highCount))).toBe(true);
  });
  it('totals every severity', () => {
    expect(severityTotals([rep('a', 1, 2), rep('b', 3, 4)])).toEqual({ critical: 4, high: 6, medium: 2, low: 2 });
    expect(severityTotals([])).toEqual({ critical: 0, high: 0, medium: 0, low: 0 });
  });
});

describe('reportsForWorkload', () => {
  const labelled = (kind: string, name: string, ns: string) => ({ metadata: { labels: { 'trivy-operator.resource.kind': kind, 'trivy-operator.resource.name': name, 'trivy-operator.resource.namespace': ns } } });
  it('matches the labels of kind, name and namespace', () => {
    const reports = [labelled('Deployment', 'web', 'ns'), labelled('Deployment', 'web', 'other'), labelled('Deployment', 'api', 'ns'), labelled('StatefulSet', 'web', 'ns')];
    expect(reportsForWorkload(reports, { kind: 'Deployment', name: 'web', namespace: 'ns' })).toHaveLength(1);
  });
});
