export interface ModuleKind {
  group: string;
  version: string;
  resource: string;
  kind: string;
  namespaced: boolean;
}

/** What `GET /clusters/{cluster}/modules` says about one module. */
export interface ModuleStatus {
  name: string;
  active: boolean;
  reason?: string;
  kinds: ModuleKind[];
}

export interface ModuleEntry {
  /** The config key of the module. */
  name: string;
  title: string;
  /** A heading the entry sits under in the sidebar. */
  group?: 'Security';
}

/** The sidebar entries of the modules, in the order of the spec (Flux has its own section). */
export const MODULE_ENTRIES: ModuleEntry[] = [
  { name: 'karpenter', title: 'Karpenter' },
  { name: 'keda', title: 'KEDA' },
  { name: 'externalSecrets', title: 'External Secrets' },
  { name: 'vaultSecrets', title: 'Vault Secrets Operator' },
  { name: 'certManager', title: 'cert-manager' },
  { name: 'gatewayApi', title: 'Gateway API' },
  { name: 'trivy', title: 'Trivy Operator', group: 'Security' },
  { name: 'permissions', title: 'Permissions and policy', group: 'Security' },
];

/** Whether an active module claims the kind, so it is not repeated under Custom Resources. */
export function claimedBy(modules: ModuleStatus[]): (group: string, kind: string) => boolean {
  const claimed = new Set(
    modules.filter((m) => m.active).flatMap((m) => m.kinds.map((k) => `${k.group}/${k.kind}`)),
  );
  return (group, kind) => claimed.has(`${group}/${kind}`);
}

/**
 * The entries to show: active modules, except those whose kinds the user cannot list. The permissions view
 * needs no CRDs, so it is always shown.
 */
export function visibleEntries(
  modules: ModuleStatus[],
  canList: (group: string, resource: string) => boolean | undefined,
): ModuleEntry[] {
  return MODULE_ENTRIES.filter((entry) => {
    const status = modules.find((m) => m.name === entry.name);
    if (!status?.active) return false;
    if (entry.name === 'permissions' || status.kinds.length === 0) return true;
    return status.kinds.some((k) => canList(k.group, k.resource) !== false);
  });
}
