import { useState } from 'react';
import { ApiError } from '../../api/client';
import { useApi } from '../../api/context';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { getArray, getString } from '../../lib/path';
import { shellTab, useDock } from '../../state/dock';
import type { K8sObject } from '../../state/live';

export const PROFILES = ['general', 'baseline', 'restricted', 'netadmin', 'sysadmin'] as const;

let serial = 0;

/** Adds an ephemeral debug container to a pod (or reuses a running one) and opens a shell in it. */
export function DebugDialog({
  cluster,
  pod,
  onClose,
}: {
  cluster: string;
  pod: K8sObject;
  onClose: () => void;
}) {
  const api = useApi();
  const open = useDock().open;
  const name = getString(pod, 'metadata.name');
  const namespace = getString(pod, 'metadata.namespace');
  const containers = [...getArray(pod, 'spec.containers'), ...getArray(pod, 'spec.initContainers')]
    .map((c) => getString(c, 'name'))
    .filter((n) => n !== '');
  const [profile, setProfile] = useState<string>('general');
  const [target, setTarget] = useState(containers[0] ?? '');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  const start = async () => {
    setBusy(true);
    setError(undefined);
    try {
      const r = await api.post<{ container: string; reused: boolean }>(
        `/clusters/${encodeURIComponent(cluster)}/debug/pod`,
        {
          namespace,
          pod: name,
          target,
          profile,
        },
      );
      open(shellTab({ cluster, namespace, pod: name, container: r.container }, ++serial));
      onClose();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Something went wrong.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={`Debug ${name}`}
      onKeyDown={(e) => e.key === 'Escape' && onClose()}
    >
      <h2>{`Debug ${name}`}</h2>
      <p>
        A debug container stays in the pod until the pod is deleted. Kubernetes doesn&apos;t allow removing
        it.
      </p>
      <label>
        Profile
        <select value={profile} onChange={(e) => setProfile(e.target.value)}>
          {PROFILES.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
      </label>
      {containers.length > 1 && (
        <label>
          Target container
          <select value={target} onChange={(e) => setTarget(e.target.value)}>
            {containers.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </select>
        </label>
      )}
      <ErrorText message={error} />
      <Button onClick={onClose}>Cancel</Button>
      <Button onClick={() => void start()} disabled={busy}>
        Add debug container
      </Button>
    </div>
  );
}
