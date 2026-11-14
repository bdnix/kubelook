import { useState } from 'react';
import { useAccess } from '../../api/access';
import { ApiError } from '../../api/client';
import { useApi } from '../../api/context';
import { useFeatures } from '../../api/me';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { getString } from '../../lib/path';
import { shellTab, useDock } from '../../state/dock';
import type { K8sObject } from '../../state/live';

let serial = 0;

/** "Node shell" on a node: starts a privileged pod there and opens a shell in it. Hidden unless the feature is on. */
export function NodeShellButton({ cluster, node }: { cluster: string; node: K8sObject }) {
  const features = useFeatures();
  const api = useApi();
  const open = useDock().open;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const access = useAccess(
    cluster,
    [{ verb: 'create', group: '', resource: 'pods', subresource: 'exec' }],
    features?.nodeShell === true,
  );
  if (features?.nodeShell !== true || access.data?.[0]?.allowed !== true) return null;

  const start = async () => {
    setBusy(true);
    setError(undefined);
    try {
      const r = await api.post<{ namespace: string; pod: string; container: string }>(
        `/clusters/${encodeURIComponent(cluster)}/debug/node`,
        { node: getString(node, 'metadata.name') },
      );
      open({
        ...shellTab(
          { cluster, namespace: r.namespace, pod: r.pod, container: r.container, nodeShell: true },
          ++serial,
        ),
        title: `Node shell: ${getString(node, 'metadata.name')}`,
      });
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Something went wrong.');
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <Button onClick={() => void start()} disabled={busy}>
        Node shell
      </Button>
      <ErrorText message={error} />
    </>
  );
}
