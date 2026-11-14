import { useCallback, useState } from 'react';
import { ApiError } from '../../api/client';
import { useApi } from '../../api/context';

export interface ActionTargetRef {
  /** The API group; the empty group is sent as `core`. */
  group: string;
  version: string;
  resource: string;
  namespace: string;
  name: string;
}

/** Runs an action (`POST /clusters/{cluster}/actions`), or a delete (`DELETE` on the object). */
export function useAction(cluster: string) {
  const api = useApi();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  const run = useCallback(
    async (action: string, target: ActionTargetRef, params?: unknown): Promise<boolean> => {
      setBusy(true);
      setError(undefined);
      const base = `/clusters/${encodeURIComponent(cluster)}`;
      const group = target.group === '' ? 'core' : target.group;
      try {
        if (action === 'delete') {
          const ns = target.namespace ? `?namespace=${encodeURIComponent(target.namespace)}` : '';
          await api.delete(
            `${base}/resources/${group}/${target.version}/${target.resource}/${encodeURIComponent(target.name)}${ns}`,
          );
        } else {
          await api.post(`${base}/actions`, { action, target: { ...target, group }, params });
        }
        return true;
      } catch (e) {
        setError(e instanceof ApiError ? e.message : 'Something went wrong.');
        return false;
      } finally {
        setBusy(false);
      }
    },
    [api, cluster],
  );

  return { run, busy, error, reset: () => setError(undefined) };
}
