import { useState } from 'react';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { useFocusTrap } from '../../lib/useFocusTrap';
import { useAction } from '../actions/useAction';
import type { Consumer } from './consumers';
import { useRef } from 'react';

/** The workloads that use a Secret, and a button that restarts them all after a confirmation. */
export function ConsumersPanel({ cluster, consumers }: { cluster: string; consumers: Consumer[] }) {
  const { run, busy, error } = useAction(cluster);
  const [asking, setAsking] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useFocusTrap(ref);
  if (consumers.length === 0) return <p>No Deployment, StatefulSet or DaemonSet uses this Secret.</p>;
  const restart = async () => {
    for (const c of consumers) {
      const ok = await run('restart', {
        group: c.group,
        version: c.version,
        resource: c.resource,
        namespace: c.namespace,
        name: c.name,
      });
      if (!ok) return;
    }
    setAsking(false);
  };
  return (
    <section aria-label="Consumers">
      <h3>Consumers</h3>
      <ul>
        {consumers.map((c) => (
          <li key={`${c.kind}/${c.name}`}>{`${c.kind} ${c.name}`}</li>
        ))}
      </ul>
      <Button onClick={() => setAsking(true)}>Restart consumers</Button>
      {asking && (
        <div
          ref={ref}
          role="dialog"
          aria-modal="true"
          aria-label={`Restart ${consumers.length} workloads`}
          tabIndex={-1}
          onKeyDown={(e) => e.key === 'Escape' && setAsking(false)}
        >
          <h2>{`Restart ${consumers.length} workloads`}</h2>
          <ul>
            {consumers.map((c) => (
              <li key={`${c.kind}/${c.name}`}>{`${c.kind} ${c.name}`}</li>
            ))}
          </ul>
          <ErrorText message={error} />
          <Button onClick={() => setAsking(false)}>Cancel</Button>
          <Button disabled={busy} onClick={() => void restart()}>
            Restart
          </Button>
        </div>
      )}
    </section>
  );
}
