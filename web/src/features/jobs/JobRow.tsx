import { Button } from '../../components/Button';
import { isActive, type Job, type JobStatus } from './useJobs';

const LABELS: Record<JobStatus, string> = {
  queued: 'Queued',
  running: 'Running',
  succeeded: 'Succeeded',
  failed: 'Failed',
  interrupted: 'Interrupted',
};

const ICONS: Record<JobStatus, string> = {
  queued: '…',
  running: '⟳',
  succeeded: '✓',
  failed: '✕',
  interrupted: '!',
};

const cap = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

/** A short title from the job's type and the name of its target. */
export function jobTitle(job: Job): string {
  const target = job.target as { name?: unknown } | null;
  const name = typeof target?.name === 'string' ? target.name : '';
  return name ? `${cap(job.type)} ${name}` : cap(job.type);
}

export interface JobRowProps {
  job: Job;
  busy: boolean;
  onOffer: (job: Job, offer: string) => void;
}

/** One job: status as text and icon, progress, message, the items that failed and the offered buttons. */
export function JobRow({ job, busy, onOffer }: JobRowProps) {
  const { progress } = job;
  const problems = (progress.items ?? []).filter((i) => i.status !== 'done');
  return (
    <li className={`job job-${job.status}`}>
      <strong>{jobTitle(job)}</strong>{' '}
      <span className="job-status">
        <span aria-hidden="true">{ICONS[job.status]}</span> {LABELS[job.status]}
      </span>
      {isActive(job) && progress.total > 0 && (
        <progress
          role="progressbar"
          aria-label={`${jobTitle(job)} progress`}
          aria-valuenow={progress.done}
          aria-valuemin={0}
          aria-valuemax={progress.total}
          value={progress.done}
          max={progress.total}
        />
      )}
      {progress.text && <p className="job-progress">{progress.text}</p>}
      {job.message && <p className="job-message">{job.message}</p>}
      {problems.length > 0 && (
        <ul className="job-items">
          {problems.map((i) => (
            <li key={i.name}>
              <span>{i.name}</span>
              {i.message && <span> {i.message}</span>}
            </li>
          ))}
        </ul>
      )}
      {job.offers.length > 0 && (
        <span className="job-offers">
          {job.offers.map((offer) => (
            <Button key={offer} disabled={busy} onClick={() => onOffer(job, offer)}>
              {cap(offer)}
            </Button>
          ))}
        </span>
      )}
    </li>
  );
}
