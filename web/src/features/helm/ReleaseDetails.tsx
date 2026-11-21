import { useState } from 'react';
import { useAccess } from '../../api/access';
import { Banner } from '../../components/Banner';
import { Button } from '../../components/Button';
import { formatAge } from '../../lib/age';
import { useNow } from '../../lib/clock';
import { ConfirmDialog } from '../actions/ConfirmDialog';
import { useHelmJob, useReleaseDetail, type HelmDetail } from './api';
import { UpgradeDialog } from './UpgradeDialog';
import { ValuesPanel } from './ValuesPanel';

type Tab = 'Values' | 'Manifest' | 'Notes' | 'Hooks' | 'History';
const TABS: Tab[] = ['Values', 'Manifest', 'Notes', 'Hooks', 'History'];
type Dialog = 'upgrade' | 'rollback' | 'uninstall' | 'test' | undefined;
const MINUTE = 60_000;

function History({ detail }: { detail: HelmDetail }) {
  const now = useNow(MINUTE);
  return (
    <table aria-label="History">
      <thead>
        <tr>
          <th>Revision</th>
          <th>Status</th>
          <th>Chart version</th>
          <th>Description</th>
          <th>Updated</th>
        </tr>
      </thead>
      <tbody>
        {detail.history.map((h) => (
          <tr key={h.revision}>
            <td>{h.revision}</td>
            <td>{h.status}</td>
            <td>{h.chartVersion}</td>
            <td>{h.description}</td>
            <td>{formatAge(h.updated, now)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function Body({ cluster, detail, tab }: { cluster: string; detail: HelmDetail; tab: Tab }) {
  switch (tab) {
    case 'Values':
      return (
        <ValuesPanel
          cluster={cluster}
          namespace={detail.namespace}
          name={detail.name}
          revision={detail.revision}
        />
      );
    case 'Manifest':
      return <pre aria-label="Manifest">{detail.manifest}</pre>;
    case 'Notes':
      return <pre aria-label="Notes">{detail.notes}</pre>;
    case 'Hooks':
      return detail.hooks.length === 0 ? (
        <p>This release has no hooks.</p>
      ) : (
        <ul aria-label="Hooks">
          {detail.hooks.map((h) => (
            <li key={h.name}>
              <strong>{h.name}</strong> {h.kind} ({h.events.join(', ')})
            </li>
          ))}
        </ul>
      );
    case 'History':
      return <History detail={detail} />;
  }
}

/** One release: values, manifest, notes, hooks and history, and the actions on it. */
export function ReleaseDetails({
  cluster,
  namespace,
  name,
  onClose,
}: {
  cluster: string;
  namespace: string;
  name: string;
  onClose: () => void;
}) {
  const query = useReleaseDetail(cluster, namespace, name);
  const access = useAccess(cluster, [{ verb: 'create', group: '', resource: 'secrets', namespace }]);
  const job = useHelmJob(cluster);
  const [tab, setTab] = useState<Tab>('Values');
  const [dialog, setDialog] = useState<Dialog>();
  const [revision, setRevision] = useState('');
  const detail = query.data;
  const allowed = access.data?.[0]?.allowed !== false;
  const close = () => {
    setDialog(undefined);
    setRevision('');
  };
  const run = async (body: Parameters<typeof job.start>[0]) => {
    if (await job.start(body)) close();
  };
  return (
    <section aria-label={`Release ${name}`}>
      <h2>{name}</h2>
      <Button onClick={onClose}>Close</Button>
      {query.isError && <Banner kind="error">{query.error.message}</Banner>}
      {!allowed && (
        <p>{`You may not create Secrets in ${namespace}, so you cannot change this release (Helm keeps its releases in Secrets).`}</p>
      )}
      {detail && (
        <>
          <div>
            <Button disabled={!allowed} onClick={() => setDialog('upgrade')}>
              Upgrade
            </Button>
            <Button disabled={!allowed} onClick={() => setDialog('rollback')}>
              Roll back
            </Button>
            <Button disabled={!allowed} onClick={() => setDialog('test')}>
              Test
            </Button>
            <Button disabled={!allowed} onClick={() => setDialog('uninstall')}>
              Uninstall
            </Button>
          </div>
          <div role="tablist" aria-label="Release">
            {TABS.map((t) => (
              <button key={t} type="button" role="tab" aria-selected={tab === t} onClick={() => setTab(t)}>
                {t}
              </button>
            ))}
          </div>
          <Body cluster={cluster} detail={detail} tab={tab} />
          {dialog === 'upgrade' && <UpgradeDialog cluster={cluster} release={detail} onClose={close} />}
          {dialog === 'rollback' && (
            <ConfirmDialog
              title={`Roll back ${name}`}
              confirmLabel="Roll back"
              busy={job.busy}
              error={job.error}
              canConfirm={revision !== ''}
              kubectl={`helm rollback ${name} ${revision || '<revision>'} -n ${namespace}`}
              copyLabel="Copy as helm"
              onCancel={close}
              onConfirm={() => void run({ action: 'rollback', namespace, name, revision: Number(revision) })}
            >
              <label htmlFor="helm-revision">Revision</label>
              <select id="helm-revision" value={revision} onChange={(e) => setRevision(e.target.value)}>
                <option value="">Choose…</option>
                {detail.history
                  .filter((h) => h.revision !== detail.revision)
                  .map((h) => (
                    <option key={h.revision} value={h.revision}>
                      {h.revision}
                    </option>
                  ))}
              </select>
            </ConfirmDialog>
          )}
          {dialog === 'uninstall' && (
            <ConfirmDialog
              title={`Uninstall ${name}`}
              confirmLabel="Uninstall"
              destructive
              busy={job.busy}
              error={job.error}
              kubectl={`helm uninstall ${name} -n ${namespace}`}
              copyLabel="Copy as helm"
              onCancel={close}
              onConfirm={() => void run({ action: 'uninstall', namespace, name })}
            >
              <p>{`This removes ${name} and the objects it created from ${namespace}.`}</p>
            </ConfirmDialog>
          )}
          {dialog === 'test' && (
            <ConfirmDialog
              title={`Test ${name}`}
              confirmLabel="Run test"
              busy={job.busy}
              error={job.error}
              kubectl={`helm test ${name} -n ${namespace}`}
              copyLabel="Copy as helm"
              onCancel={close}
              onConfirm={() => void run({ action: 'test', namespace, name })}
            >
              <p>Runs the test hooks of the release as pods in its namespace.</p>
            </ConfirmDialog>
          )}
        </>
      )}
    </section>
  );
}
