import type { DiffLine } from '../../lib/diff';

/** The line diff of what a save would change, from the server's dry run. */
export function SavePreview({ diff }: { diff: DiffLine[] }) {
  const changed = diff.some((l) => l.type !== 'same');
  if (!changed) return <p>The save would change nothing.</p>;
  return (
    <pre aria-label="Preview of the change" className="diff">
      {diff.map((l, i) => (
        <span key={i} className={`diff-${l.type}`}>
          {`${l.type === 'add' ? '+' : l.type === 'del' ? '-' : ' '} ${l.text}\n`}
        </span>
      ))}
    </pre>
  );
}
