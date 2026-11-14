import { useQuery } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import { findResource, useDiscovery, type Discovery } from '../../api/discovery';
import { useApi } from '../../api/context';
import type { ApiClient } from '../../api/client';
import { ownerChain, groupOf, type ChainLink, type OwnerRef } from '../../lib/owners';
import { getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { useLive } from '../../state/useLive';

const core = (group: string) => (group === '' ? 'core' : group);

/** The live items of a kind in a namespace, empty until the list arrives or while `enabled` is false. */
export function useKindItems(
  cluster: string,
  group: string,
  kind: string,
  namespace: string,
  enabled: boolean,
): K8sObject[] {
  const discovery = useDiscovery(cluster, enabled);
  const resource = discovery.data ? findResource(discovery.data, group, kind) : undefined;
  const live = useLive(
    enabled && resource
      ? {
          cluster,
          group: core(resource.group),
          version: resource.version,
          resource: resource.resource,
          namespace,
        }
      : undefined,
  );
  return enabled && resource ? live.items : [];
}

/** The discovery entry (version, resource name) of a kind, once discovery has loaded. */
export function useResourceInfo(cluster: string, group: string, kind: string) {
  const discovery = useDiscovery(cluster);
  return discovery.data ? findResource(discovery.data, group, kind) : undefined;
}

export type ObjectState =
  | { state: 'loading' }
  | { state: 'found'; object: K8sObject }
  | { state: 'deleted'; object: K8sObject }
  | { state: 'missing' }
  | { state: 'error'; message: string; code: string };

/**
 * One object, kept live. It is watched through its own list (`metadata.name=<name>`), so it stays live
 * when the list view behind it is filtered. An object that disappears after it was seen was deleted.
 */
export function useObject(
  cluster: string,
  group: string,
  kind: string,
  namespace: string,
  name: string,
): ObjectState {
  const discovery = useDiscovery(cluster);
  const resource = discovery.data ? findResource(discovery.data, group, kind) : undefined;
  const live = useLive(
    resource
      ? {
          cluster,
          group: core(resource.group),
          version: resource.version,
          resource: resource.resource,
          namespace,
          fieldSelector: `metadata.name=${name}`,
        }
      : undefined,
  );
  const last = useRef<K8sObject | undefined>(undefined);
  const found = live.items.find((o) => getString(o, 'metadata.name') === name);
  useEffect(() => {
    if (found) last.current = found;
  }, [found]);
  if (discovery.isError) return { state: 'error', message: discovery.error.message, code: 'internal' };
  if (!resource) return discovery.isPending ? { state: 'loading' } : { state: 'missing' };
  if (live.status === 'error') {
    return {
      state: 'error',
      message: live.error?.message ?? 'Something went wrong.',
      code: live.error?.code ?? 'internal',
    };
  }
  if (found) return { state: 'found', object: found };
  if (live.status === 'loading') return { state: 'loading' };
  return last.current ? { state: 'deleted', object: last.current } : { state: 'missing' };
}

async function fetchOwner(
  api: ApiClient,
  discovery: Discovery,
  cluster: string,
  ref: OwnerRef,
  namespace: string,
) {
  const resource = findResource(discovery, groupOf(ref.apiVersion), ref.kind);
  if (!resource) return undefined;
  const ns = resource.namespaced ? `?namespace=${encodeURIComponent(namespace)}` : '';
  try {
    return await api.get<K8sObject>(
      `/clusters/${encodeURIComponent(cluster)}/resources/${core(resource.group)}/${resource.version}/${resource.resource}/${encodeURIComponent(ref.name)}${ns}`,
    );
  } catch {
    return undefined; // a missing or forbidden owner ends the chain
  }
}

/** The owners of the object, nearest first, read once per object (not live). */
export function useOwnerChain(cluster: string, object: K8sObject | undefined): ChainLink[] {
  const api = useApi();
  const discovery = useDiscovery(cluster);
  const uid = object ? getString(object, 'metadata.uid') : '';
  const namespace = object ? getString(object, 'metadata.namespace') : '';
  const objectRef = useRef(object);
  useEffect(() => {
    objectRef.current = object;
  });
  const query = useQuery({
    queryKey: ['owners', cluster, uid],
    enabled: object !== undefined && discovery.data !== undefined,
    staleTime: 60_000,
    queryFn: () => {
      const start = objectRef.current;
      const disc = discovery.data;
      if (!start || !disc) return [];
      return ownerChain(start, (ref) => fetchOwner(api, disc, cluster, ref, namespace));
    },
  });
  return query.data ?? [];
}

/** The element that had focus before the drawer opened; focus moves into the drawer and back on close. */
export function useFocusReturn(): React.RefObject<HTMLElement | null> {
  const container = useRef<HTMLElement | null>(null);
  const [previous] = useState(() => (typeof document === 'undefined' ? null : document.activeElement));
  useEffect(() => {
    container.current?.focus();
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
    };
  }, [previous]);
  return container;
}
