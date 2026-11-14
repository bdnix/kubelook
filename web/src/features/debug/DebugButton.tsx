import { useState } from 'react';
import { useAccess } from '../../api/access';
import { Button } from '../../components/Button';
import { getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { DebugDialog } from './DebugDialog';

/** "Debug…" on a pod. Hidden unless the user may patch `pods/ephemeralcontainers`. */
export function DebugButton({ cluster, pod }: { cluster: string; pod: K8sObject }) {
  const [open, setOpen] = useState(false);
  const check = {
    verb: 'patch',
    group: '',
    resource: 'pods',
    subresource: 'ephemeralcontainers',
    namespace: getString(pod, 'metadata.namespace'),
    name: getString(pod, 'metadata.name'),
  };
  const access = useAccess(cluster, [check]);
  if (access.data?.[0]?.allowed !== true) return null;
  return (
    <>
      <Button onClick={() => setOpen(true)}>Debug…</Button>
      {open && <DebugDialog cluster={cluster} pod={pod} onClose={() => setOpen(false)} />}
    </>
  );
}
