import { describe, expect, it } from 'vitest';
import { getKind, knownKinds } from './registry';

describe('kind registry', () => {
  it('finds the pod descriptor by group and kind; core is the empty group', () => {
    const a = getKind('core', 'Pod');
    const b = getKind('', 'Pod');
    expect(a).toBe(b);
    expect(a.title).toBe('Pods');
    expect(knownKinds()).toContain(a);
  });

  it('unknown kinds fall back to Name, Namespace and Age', () => {
    const k = getKind('example.io', 'Widget');
    expect(k.columns.map((c) => c.title)).toEqual(['Name', 'Namespace', 'Age']);
    const widget = { metadata: { name: 'w', namespace: 'ns', creationTimestamp: '2026-11-14T11:00:00Z' } };
    const now = Date.parse('2026-11-14T12:00:00Z');
    expect(k.columns.map((c) => c.value(widget, now))).toEqual(['w', 'ns', '1h']);
    expect(k.group).toBe('example.io');
    expect(k.kind).toBe('Widget');
    expect(k.status).toBeUndefined();
  });
});
