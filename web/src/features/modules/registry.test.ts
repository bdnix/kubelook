import { describe, expect, it } from 'vitest';
import { claimedBy, MODULE_ENTRIES, visibleEntries, type ModuleStatus } from './registry';

const status = (name: string, active: boolean, kinds: [string, string][] = []): ModuleStatus => ({
  name,
  active,
  kinds: kinds.map(([group, kind]) => ({
    group,
    version: 'v1',
    resource: `${kind.toLowerCase()}s`,
    kind,
    namespaced: true,
  })),
});

describe('MODULE_ENTRIES', () => {
  it('follow the order of the spec, with Security holding Trivy and Permissions', () => {
    expect(MODULE_ENTRIES.map((e) => `${e.group ?? ''}|${e.title}`)).toEqual([
      '|Karpenter',
      '|KEDA',
      '|External Secrets',
      '|Vault Secrets Operator',
      '|cert-manager',
      '|Gateway API',
      'Security|Trivy Operator',
      'Security|Permissions and policy',
    ]);
  });
});

describe('claimedBy', () => {
  it('claims the kinds of active modules only', () => {
    const claimed = claimedBy([
      status('keda', true, [['keda.sh', 'ScaledObject']]),
      status('trivy', false, [['aquasecurity.github.io', 'VulnerabilityReport']]),
    ]);
    expect(claimed('keda.sh', 'ScaledObject')).toBe(true);
    expect(claimed('keda.sh', 'ScaledJob')).toBe(false);
    expect(claimed('aquasecurity.github.io', 'VulnerabilityReport')).toBe(false);
  });
});

describe('visibleEntries', () => {
  const modules = [
    status('karpenter', true, [['karpenter.sh', 'NodePool']]),
    status('keda', false),
    status('permissions', true),
    status('trivy', true, [['aquasecurity.github.io', 'VulnerabilityReport']]),
  ];
  const all = () => true;

  it('are the active modules only', () => {
    expect(visibleEntries(modules, all).map((e) => e.name)).toEqual(['karpenter', 'trivy', 'permissions']);
  });

  it('hide a module whose kinds the user cannot list, but never Permissions', () => {
    const canList = (group: string) => group !== 'karpenter.sh';
    expect(visibleEntries(modules, canList).map((e) => e.name)).toEqual(['trivy', 'permissions']);
  });

  it('keep a module while the answer is not known', () => {
    expect(visibleEntries(modules, () => undefined).map((e) => e.name)).toContain('karpenter');
  });
});
