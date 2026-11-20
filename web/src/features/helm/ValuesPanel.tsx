import { useState } from 'react';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { useRevealValues } from './api';

/**
 * The values of a release. They are not loaded with the release: Reveal asks the audited endpoint, and
 * leaving the panel hides them again.
 */
export function ValuesPanel({
  cluster,
  namespace,
  name,
  revision,
}: {
  cluster: string;
  namespace: string;
  name: string;
  revision: number;
}) {
  const reveal = useRevealValues(cluster, namespace, name);
  const [shown, setShown] = useState(false);
  if (!shown || !reveal.data) {
    return (
      <div>
        <p>
          The values may hold secrets, so they are shown only when you ask. Revealing them is recorded in the
          audit log.
        </p>
        <Button
          onClick={() => reveal.mutate(revision, { onSuccess: () => setShown(true) })}
          disabled={reveal.isPending}
        >
          Reveal values
        </Button>
        <ErrorText message={reveal.error?.message} />
      </div>
    );
  }
  return (
    <div>
      <Button onClick={() => setShown(false)}>Hide values</Button>
      <h3>Values you supplied</h3>
      <pre>{reveal.data.userValues}</pre>
      <h3>Computed values</h3>
      <pre>{reveal.data.computedValues}</pre>
    </div>
  );
}
