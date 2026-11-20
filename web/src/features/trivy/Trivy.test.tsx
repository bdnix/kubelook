import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, renderApp } from '../../test/render';
import { exportFindings } from './exportFindings';
import { ReportDetails } from './ReportDetails';
import { TrivyOverviewView } from './TrivyOverview';
import { WorkloadTrivySection } from './WorkloadTrivySection';

const vuln = {
  metadata: { name: 'r1', namespace: 'ns', labels: { 'trivy-operator.resource.kind': 'Deployment', 'trivy-operator.resource.name': 'web', 'trivy-operator.resource.namespace': 'ns' } },
  report: {
    summary: { criticalCount: 1, highCount: 1, mediumCount: 0, lowCount: 0 },
    vulnerabilities: [
      { vulnerabilityID: 'CVE-1', resource: 'openssl', installedVersion: '1.0', fixedVersion: '1.1', severity: 'CRITICAL', title: '=HYPERLINK("http://evil")' },
      { vulnerabilityID: 'CVE-2', resource: 'zlib', installedVersion: '2.0', fixedVersion: '', severity: 'HIGH', title: 'meh' },
    ],
  },
};

describe('ReportDetails', () => {
  it('lists the findings with their columns', () => {
    renderApp(<ReportDetails cluster="c" kind="VulnerabilityReport" group="aquasecurity.github.io" version="v1alpha1" resource="vulnerabilityreports" object={vuln} />);
    const table = screen.getByRole('table', { name: 'Findings' });
    expect(within(table).getByText('CVE-1')).toBeInTheDocument();
    expect(within(table).getByText('openssl')).toBeInTheDocument();
    expect(within(table).getByText('1.0 → 1.1')).toBeInTheDocument();
    expect(within(table).getAllByRole('row')).toHaveLength(3);
  });

  it('asks before it deletes the report so the operator scans again, and then does', async () => {
    const posted: unknown[] = [];
    const fetch = vi.fn((_u: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') posted.push(JSON.parse(String(init.body)));
      return Promise.resolve(jsonResponse({}));
    });
    renderApp(<ReportDetails cluster="c" kind="VulnerabilityReport" group="aquasecurity.github.io" version="v1alpha1" resource="vulnerabilityreports" object={vuln} />, { client: createClient({ fetch }) });
    await userEvent.click(screen.getByRole('button', { name: 'Rescan' }));
    const dialog = await screen.findByRole('dialog', { name: 'Rescan r1' });
    expect(dialog).toHaveTextContent('The operator should scan again after the report is deleted (not confirmed for every operator version).');
    expect(posted).toHaveLength(0);
    await userEvent.click(within(dialog).getByRole('button', { name: 'Delete the report' }));
    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0]).toEqual({ action: 'trivyRescan', target: { group: 'aquasecurity.github.io', version: 'v1alpha1', resource: 'vulnerabilityreports', namespace: 'ns', name: 'r1' } });
  });

  it('exports the findings as CSV and JSON, with formulas neutralised in titles', async () => {
    const saved: { name: string; text: string }[] = [];
    const save = (name: string, bytes: Uint8Array) => saved.push({ name, text: new TextDecoder().decode(bytes) });
    renderApp(<ReportDetails cluster="c" kind="VulnerabilityReport" group="aquasecurity.github.io" version="v1alpha1" resource="vulnerabilityreports" object={vuln} saveFile={save} />);
    await userEvent.click(screen.getByRole('button', { name: 'Export CSV' }));
    expect(saved[0]?.name).toBe('r1-findings.csv');
    expect(saved[0]?.text).toContain("'=HYPERLINK");
    expect(saved[0]?.text).not.toMatch(/,=HYPERLINK/);
    await userEvent.click(screen.getByRole('button', { name: 'Export JSON' }));
    expect(saved[1]?.name).toBe('r1-findings.json');
    expect(JSON.parse(saved[1]?.text ?? '[]')).toHaveLength(2);
  });
});

describe('exportFindings', () => {
  it('builds the CSV with a header and one row per finding', () => {
    const csv = exportFindings('csv', [{ id: 'CVE-1', subject: 's', detail: 'd', severity: 'HIGH', title: 't' }]);
    expect(csv).toContain('ID,Subject,Detail,Severity,Title');
    expect(csv).toContain('CVE-1,s,d,HIGH,t');
  });
});

describe('TrivyOverviewView', () => {
  const reports = [
    { kind: 'VulnerabilityReport', group: 'aquasecurity.github.io', object: vuln },
    { kind: 'VulnerabilityReport', group: 'aquasecurity.github.io', object: { metadata: { name: 'r2', namespace: 'ns' }, report: { summary: { criticalCount: 0, highCount: 9, mediumCount: 0, lowCount: 0 } } } },
  ];
  it('shows the totals and the worst reports with links', () => {
    renderApp(<TrivyOverviewView cluster="c" reports={reports} />);
    const totals = screen.getByRole('region', { name: 'Totals' });
    expect(totals).toHaveTextContent('Critical 1');
    expect(totals).toHaveTextContent('High 10');
    const worst = screen.getByRole('list', { name: 'Worst reports' });
    const items = within(worst).getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('r1');
    expect(within(items[0] as HTMLElement).getByRole('link', { name: 'r1' })).toHaveAttribute('href', '/c/c/ns/ns/aquasecurity.github.io/VulnerabilityReport/r1');
  });
});

describe('WorkloadTrivySection', () => {
  it('shows the reports of the workload with their counts, and nothing for a workload without reports', () => {
    const { rerender } = renderApp(<WorkloadTrivySection cluster="c" reports={[{ kind: 'VulnerabilityReport', group: 'aquasecurity.github.io', object: vuln }]} />);
    const section = screen.getByRole('region', { name: 'Trivy' });
    expect(section).toHaveTextContent('VulnerabilityReport');
    expect(section).toHaveTextContent('1 critical');
    expect(within(section).getByRole('link', { name: 'r1' })).toHaveAttribute('href', '/c/c/ns/ns/aquasecurity.github.io/VulnerabilityReport/r1');
    rerender(<WorkloadTrivySection cluster="c" reports={[]} />);
    expect(screen.queryByRole('region', { name: 'Trivy' })).toBeNull();
  });
});
