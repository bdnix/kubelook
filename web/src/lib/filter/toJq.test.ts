import { describe, expect, it } from 'vitest';
import { parseFilter, type Filter } from './parse';
import { toKubectl } from './toJq';

const f = (text: string): Filter => {
  const r = parseFilter(text);
  if (!r.ok) throw new Error(r.error.message);
  return r.filter;
};
const jq = (text: string, namespace: string | null = 'default', resource = 'pods') =>
  toKubectl(f(text), { resource, namespace }, 'jq');

const head = "kubectl get pods -n default -o json | jq '.items[] | select(";

describe('toKubectl (jq)', () => {
  it.each([
    ['status.phase = Running', 'any(.status.phase; . != null and tostring == "Running")'],
    ['status.phase != Running', 'any(.status.phase; tostring != "Running")'],
    [
      'metadata.name contains web',
      'any(.metadata.name; (type | IN("string","number","boolean")) and (tostring | contains("web")))',
    ],
    [
      'metadata.name regex ^web-\\d+$',
      'any(.metadata.name; (type | IN("string","number","boolean")) and (tostring | test("^web-\\\\d+$")))',
    ],
    [
      'status.phase in (Running, Pending)',
      'any(.status.phase; (type | IN("string","number","boolean")) and (tostring | IN("Running","Pending")))',
    ],
    ['metadata.deletionTimestamp exists', 'any(.metadata.deletionTimestamp; . != null)'],
    [
      'metadata.creationTimestamp older than 7d',
      'any(.metadata.creationTimestamp; type == "string" and (try (fromdateiso8601 < (now - 604800)) catch false))',
    ],
    [
      'metadata.creationTimestamp newer than 15m',
      'any(.metadata.creationTimestamp; type == "string" and (try (fromdateiso8601 > (now - 900)) catch false))',
    ],
    [
      'metadata.creationTimestamp older than 2026-11-01T00:00:00Z',
      'any(.metadata.creationTimestamp; type == "string" and (try (fromdateiso8601 < ("2026-11-01T00:00:00Z" | fromdateiso8601)) catch false))',
    ],
  ])('%s', (text, expr) => {
    expect(jq(text)).toBe(`${head}${expr})'`);
  });

  it('paths with [*] and indexes become jq generators', () => {
    expect(jq('spec.containers[*].image = nginx')).toBe(
      `${head}any(.spec.containers[]?.image; . != null and tostring == "nginx"))'`,
    );
    expect(jq('spec.containers[0].name = app')).toBe(
      `${head}any(.spec.containers[0]?.name; . != null and tostring == "app"))'`,
    );
  });

  it('joins with and, or and parentheses', () => {
    expect(jq('a = 1 OR b = 2 AND c = 3')).toBe(
      `${head}any(.a; . != null and tostring == "1") or (any(.b; . != null and tostring == "2") and any(.c; . != null and tostring == "3")))'`,
    );
  });

  it('all namespaces, one namespace, or none for cluster-scoped kinds', () => {
    expect(jq('a = 1', '')).toContain('kubectl get pods -A -o json | jq');
    expect(jq('a = 1', 'kube-system')).toContain('kubectl get pods -n kube-system -o json | jq');
    expect(jq('a = 1', null, 'nodes')).toContain('kubectl get nodes -o json | jq');
  });

  it('escapes jq strings and shell quotes', () => {
    expect(jq('metadata.name = "it\'s \\"x\\" \\\\"')).toContain(`tostring == "it'\\''s \\"x\\" \\\\"`);
    const cmd = jq('a = "$(rm -rf /)"');
    expect(cmd).toContain('"$(rm -rf /)"');
    expect(cmd.startsWith("kubectl get pods -n default -o json | jq '")).toBe(true);
    expect(cmd.endsWith(")'")).toBe(true);
  });

  it('quotes resource and namespace names', () => {
    expect(toKubectl(f('a = 1'), { resource: 'deployments.apps', namespace: 'my ns' }, 'jq')).toContain(
      "kubectl get deployments.apps -n 'my ns' -o json",
    );
  });

  it('the yq variant reads YAML', () => {
    const cmd = toKubectl(f('status.phase = Running'), { resource: 'pods', namespace: 'default' }, 'yq');
    expect(cmd).toBe(
      `kubectl get pods -n default -o yaml | yq '.items[] | select(any(.status.phase; . != null and tostring == "Running"))'`,
    );
  });
});
