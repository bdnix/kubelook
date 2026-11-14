import { useCallback, useState } from 'react';
import { ApiError } from '../../api/client';
import { useApi } from '../../api/context';
import { base64ToBytes } from '../../lib/bytes';

/**
 * Revealed values of one Secret, kept only in this component's state: they are gone when the drawer closes.
 * Every reveal is a request the server audits.
 */
export function useReveal(cluster: string, namespace: string, name: string) {
  const api = useApi();
  const [values, setValues] = useState<Record<string, Uint8Array>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  const reveal = useCallback(
    async (keys?: string[]): Promise<Record<string, Uint8Array> | undefined> => {
      setBusy(true);
      setError(undefined);
      try {
        const r = await api.post<{ data: Record<string, string> }>(
          `/clusters/${encodeURIComponent(cluster)}/secrets/reveal`,
          {
            namespace,
            name,
            ...(keys ? { keys } : {}),
          },
        );
        const decoded = Object.fromEntries(Object.entries(r.data).map(([k, v]) => [k, base64ToBytes(v)]));
        setValues((v) => ({ ...v, ...decoded }));
        return decoded;
      } catch (e) {
        setError(e instanceof ApiError ? e.message : 'Something went wrong.');
        return undefined;
      } finally {
        setBusy(false);
      }
    },
    [api, cluster, namespace, name],
  );

  const hide = useCallback((key?: string) => {
    setValues((v) =>
      key === undefined ? {} : Object.fromEntries(Object.entries(v).filter(([k]) => k !== key)),
    );
  }, []);

  return { values, reveal, hide, busy, error };
}
