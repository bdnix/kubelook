import { get, getArray, getString } from './path';
import type { K8sObject } from '../state/live';

export interface OwnerRef {
  apiVersion: string;
  kind: string;
  name: string;
  uid: string;
  controller: boolean;
}

export interface ChainLink {
  ref: OwnerRef;
  /** The owner itself, when it is known; absent when it is missing from the lists the drawer holds. */
  object?: K8sObject;
}

/** How many owners the chain follows at most. */
export const MAX_OWNER_LEVELS = 5;

export function ownerRefs(obj: K8sObject): OwnerRef[] {
  return getArray(obj, 'metadata.ownerReferences').map((r) => ({
    apiVersion: getString(r, 'apiVersion'),
    kind: getString(r, 'kind'),
    name: getString(r, 'name'),
    uid: getString(r, 'uid'),
    controller: get(r, 'controller') === true,
  }));
}

/** The group of an `apiVersion` such as `apps/v1`; the core group is empty. */
export function groupOf(apiVersion: string): string {
  const i = apiVersion.indexOf('/');
  return i < 0 ? '' : apiVersion.slice(0, i);
}

/**
 * The chain of owners of `obj`, nearest first: Pod → ReplicaSet → Deployment. The controller owner is
 * followed when there is one, else the first. The chain stops at `MAX_OWNER_LEVELS` levels, or at the first
 * owner `lookup` cannot find (that owner is still the last link, without its object).
 */
export async function ownerChain(
  obj: K8sObject,
  lookup: (ref: OwnerRef) => Promise<K8sObject | undefined>,
): Promise<ChainLink[]> {
  const chain: ChainLink[] = [];
  const seen = new Set<string>();
  let current = obj;
  while (chain.length < MAX_OWNER_LEVELS) {
    const refs = ownerRefs(current);
    const ref = refs.find((r) => r.controller) ?? refs[0];
    if (!ref || seen.has(ref.uid)) break;
    seen.add(ref.uid);
    const owner = await lookup(ref);
    chain.push(owner ? { ref, object: owner } : { ref });
    if (!owner) break;
    current = owner;
  }
  return chain;
}
