import { ErrorText } from '../../components/ErrorText';
import { JobRow } from './JobRow';
import { useJobs, useRetryJob, type Job } from './useJobs';

export interface JobsPanelProps {
  /** Called for an offer other than "retry" (rollback, uninstall): those belong to the feature that made the job. */
  onOffer?: (job: Job, offer: string) => void;
}

/** The user's jobs, shown above the dock tabs so they are visible whichever tab is active. Renders nothing without jobs. */
export function JobsPanel({ onOffer }: JobsPanelProps) {
  const jobs = useJobs();
  const retry = useRetryJob();
  const list = jobs.data ?? [];
  if (list.length === 0) return null;
  const handle = (job: Job, offer: string) => {
    if (offer === 'retry') retry.mutate(job.id);
    else onOffer?.(job, offer);
  };
  return (
    <section className="jobs" aria-label="Jobs">
      <ul>
        {list.map((job) => (
          <JobRow key={job.id} job={job} busy={retry.isPending} onOffer={handle} />
        ))}
      </ul>
      <ErrorText message={retry.error?.message} />
    </section>
  );
}
