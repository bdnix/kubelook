import { useState } from 'react';
import { useFeatures } from '../../api/me';
import { Banner } from '../../components/Banner';
import { Button } from '../../components/Button';
import { formatAge } from '../../lib/age';
import { useNow } from '../../lib/clock';
import { JobRow } from '../jobs/JobRow';
import { useJobs, useRetryJob, type Job } from '../jobs/useJobs';
import { useHelmJob, useReleases } from './api';
import { InstallDialog } from './InstallDialog';
import { ReleaseDetails } from './ReleaseDetails';
import { RepositoriesPanel } from './RepositoriesPanel';

const MINUTE = 60_000;
const COLUMNS = ['Name', 'Namespace', 'Status', 'Chart', 'App version', 'Revision', 'Updated'];

/** The progress rows of the Helm jobs, with the buttons a job offers. */
function HelmJobs({ cluster }: { cluster: string }) {
  const jobs = useJobs();
  const retry = useRetryJob();
  const start = useHelmJob(cluster);
  const list = (jobs.data ?? []).filter((j) => j.type.startsWith('helm-') && j.cluster === cluster);
  if (list.length === 0) return null;
  const offer = (job: Job, what: string) => {
    const t = job.target as { name: string; namespace: string; startRevision: number };
    if (what === 'retry') retry.mutate(job.id);
    else if (what === 'rollback')
      void start.start({
        action: 'rollback',
        namespace: t.namespace,
        name: t.name,
        revision: t.startRevision,
      });
    else if (what === 'uninstall')
      void start.start({ action: 'uninstall', namespace: t.namespace, name: t.name });
  };
  return (
    <section aria-label="Helm jobs">
      <ul>
        {list.map((j) => (
          <JobRow key={j.id} job={j} busy={retry.isPending || start.busy} onOffer={offer} />
        ))}
      </ul>
    </section>
  );
}

/** The Helm releases of a cluster. Releases cannot be watched, so the list is read every 15 s. */
export function ReleasesPage({ cluster }: { cluster: string }) {
  const releases = useReleases(cluster);
  const features = useFeatures();
  const now = useNow(MINUTE);
  const [selected, setSelected] = useState<{ namespace: string; name: string }>();
  const [installing, setInstalling] = useState(false);
  const helmVersion = features?.helmVersion ?? 4;
  return (
    <>
      <h1>Helm</h1>
      <p>{`This console uses Helm ${helmVersion}. Releases made by Helm 3 can be managed here too.`}</p>
      <Button onClick={() => setInstalling(true)}>Install a chart</Button>
      {installing && <InstallDialog cluster={cluster} onClose={() => setInstalling(false)} />}
      <HelmJobs cluster={cluster} />
      {releases.isError && <Banner kind="error">{releases.error.message}</Banner>}
      {releases.data && releases.data.length === 0 && <p>There are no Helm releases in this cluster.</p>}
      {releases.data && releases.data.length > 0 && (
        <table aria-label="Releases">
          <thead>
            <tr>
              {COLUMNS.map((c) => (
                <th key={c}>{c}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {releases.data.map((r) => (
              <tr key={`${r.namespace}/${r.name}`}>
                <td>
                  <button type="button" onClick={() => setSelected({ namespace: r.namespace, name: r.name })}>
                    {r.name}
                  </button>
                </td>
                <td>{r.namespace}</td>
                <td>{r.status}</td>
                <td>{`${r.chart} ${r.chartVersion}`}</td>
                <td>{r.appVersion}</td>
                <td>{r.revision}</td>
                <td>{formatAge(r.updated, now)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {selected && (
        <ReleaseDetails
          key={`${selected.namespace}/${selected.name}`}
          cluster={cluster}
          namespace={selected.namespace}
          name={selected.name}
          onClose={() => setSelected(undefined)}
        />
      )}
      <RepositoriesPanel cluster={cluster} />
    </>
  );
}
