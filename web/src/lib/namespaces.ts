import type { K8sObject } from '../state/live';

/** More selected namespaces than this are watched as one all-namespaces list and filtered in the browser. */
export const MAX_WATCHED_NAMESPACES = 10;

export interface SubscriptionPlan {
  /** The namespaces to watch, one watch each; the empty string is all namespaces. */
  namespaces: string[];
  /** When set, only the objects of these namespaces are shown. */
  clientFilter: Set<string> | undefined;
}

export function subscriptionPlan(selected: string[]): SubscriptionPlan {
  const unique = [...new Set(selected)].sort();
  if (unique.length === 0) return { namespaces: [''], clientFilter: undefined };
  if (unique.length > MAX_WATCHED_NAMESPACES) return { namespaces: [''], clientFilter: new Set(unique) };
  return { namespaces: unique, clientFilter: undefined };
}

export function keepNamespaces(items: K8sObject[], filter: Set<string> | undefined): K8sObject[] {
  if (!filter) return items;
  return items.filter((o) =>
    filter.has(String((o.metadata as { namespace?: unknown } | undefined)?.namespace ?? '')),
  );
}
