import { useState } from 'react';
import { Button } from '../../components/Button';
import { encodeUtf8 } from '../../lib/bytes';
import { getString } from '../../lib/path';
import { browserSaveFile, type SaveFile } from '../../lib/saveFile';
import type { K8sObject } from '../../state/live';
import { ConfirmDialog } from '../actions/ConfirmDialog';
import { useAction } from '../actions/useAction';
import { exportFindings } from './exportFindings';
import { findingsOf } from './findings';
import { rescanConfirmed } from '../../kinds/trivy';

export interface ReportDetailsProps {
  cluster: string;
  kind: string;
  group: string;
  version: string;
  resource: string;
  object: K8sObject;
  saveFile?: SaveFile;
}

/** The findings of one Trivy report, with export and a rescan. */
export function ReportDetails({
  cluster,
  kind,
  group,
  version,
  resource,
  object,
  saveFile = browserSaveFile,
}: ReportDetailsProps) {
  const [asking, setAsking] = useState(false);
  const { run, busy, error } = useAction(cluster);
  const name = getString(object, 'metadata.name');
  const namespace = getString(object, 'metadata.namespace');
  const findings = findingsOf(kind, object);
  const save = (format: 'csv' | 'json') =>
    saveFile(`${name}-findings.${format}`, encodeUtf8(exportFindings(format, findings)));
  return (
    <section aria-label="Findings">
      <h3>Findings</h3>
      <div>
        <Button onClick={() => setAsking(true)}>Rescan</Button>
        <Button onClick={() => save('csv')}>Export CSV</Button>
        <Button onClick={() => save('json')}>Export JSON</Button>
      </div>
      {findings.length === 0 ? (
        <p>This report has no findings.</p>
      ) : (
        <table aria-label="Findings">
          <thead>
            <tr>
              {['ID', 'Subject', 'Detail', 'Severity', 'Title'].map((h) => (
                <th key={h}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {findings.map((f, i) => (
              <tr key={`${f.id}-${i}`}>
                <td>{f.id}</td>
                <td>{f.subject}</td>
                <td>{f.detail}</td>
                <td>{f.severity}</td>
                <td>{f.title}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {asking && (
        <ConfirmDialog
          title={`Rescan ${name}`}
          confirmLabel="Delete the report"
          destructive
          busy={busy}
          error={error}
          kubectl={`kubectl delete ${resource}.${group} ${name}${namespace ? ` -n ${namespace}` : ''}`}
          onCancel={() => setAsking(false)}
          onConfirm={() =>
            void run('trivyRescan', { group, version, resource, namespace, name }).then(
              (ok) => ok && setAsking(false),
            )
          }
        >
          <p>
            {rescanConfirmed(kind)
              ? 'The operator scans again after the report is deleted (read in the operator source).'
              : 'Not confirmed for this kind: the operator may not create the report again.'}
          </p>
        </ConfirmDialog>
      )}
    </section>
  );
}
