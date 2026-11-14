import { describe, expect, it } from 'vitest';
import { MAX_WATCHED_NAMESPACES, keepNamespaces, subscriptionPlan } from './namespaces';

describe('subscriptionPlan', () => {
  it('with no namespace selected watches all namespaces', () => {
    expect(subscriptionPlan([])).toEqual({ namespaces: [''], clientFilter: undefined });
  });

  it('watches each selected namespace, once, sorted', () => {
    expect(subscriptionPlan(['b', 'a', 'b'])).toEqual({ namespaces: ['a', 'b'], clientFilter: undefined });
  });

  it('ten namespaces are still one watch each', () => {
    const ten = Array.from({ length: 10 }, (_, i) => `ns${i}`);
    expect(subscriptionPlan(ten).namespaces).toHaveLength(10);
    expect(MAX_WATCHED_NAMESPACES).toBe(10);
  });

  it('more than ten fall back to one all-namespaces watch filtered on the client', () => {
    const eleven = Array.from({ length: 11 }, (_, i) => `ns${i}`);
    const plan = subscriptionPlan(eleven);
    expect(plan.namespaces).toEqual(['']);
    expect(plan.clientFilter).toEqual(new Set(eleven));
  });
});

describe('keepNamespaces', () => {
  const o = (ns: string) => ({ metadata: { namespace: ns, name: 'x' } });
  it('keeps the objects of the selected namespaces, and everything without a filter', () => {
    const items = [o('a'), o('b'), o('c')];
    expect(keepNamespaces(items, new Set(['a', 'c']))).toEqual([o('a'), o('c')]);
    expect(keepNamespaces(items, undefined)).toBe(items);
  });
});
