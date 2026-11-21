import { toCsv } from '../../lib/csv';
import type { Finding } from './findings';

/** The findings as CSV (formulas neutralised) or JSON. */
export function exportFindings(format: 'csv' | 'json', findings: Finding[]): string {
  if (format === 'json') return JSON.stringify(findings, null, 2);
  return toCsv(
    ['ID', 'Subject', 'Detail', 'Severity', 'Title'],
    findings.map((f) => [f.id, f.subject, f.detail, f.severity, f.title]),
  );
}
