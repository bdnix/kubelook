import type { Bucket } from './triage';

const time = (ms: number) => new Date(ms).toISOString().slice(11, 16);

/** The warning events of the cluster per five minutes. */
export function WarningTimeline({ buckets }: { buckets: Bucket[] }) {
  const max = Math.max(1, ...buckets.map((b) => b.count));
  return (
    <section aria-label="Warning events">
      <h2>Warning events</h2>
      {buckets.length === 0 ? (
        <p>No warnings in the events the cluster holds.</p>
      ) : (
        <p>Only the events the cluster still holds are counted.</p>
      )}
      <ul aria-label="Warnings per five minutes">
        {buckets.map((b) => (
          <li key={b.start}>
            {`${time(b.start)} UTC `}
            <progress aria-label={`${time(b.start)} UTC`} value={b.count} max={max} />
            {` ${b.count}`}
          </li>
        ))}
      </ul>
    </section>
  );
}
