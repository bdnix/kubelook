import { expect } from 'vitest';
import type { KindDescriptor } from './types';

export const NOW = Date.parse('2026-11-14T12:00:00Z');
export const created = '2026-11-14T11:00:00Z'; // one hour before NOW

/** The column values of an object, by column id. */
export function row(kind: KindDescriptor, obj: Record<string, unknown>): Record<string, string | number> {
  return Object.fromEntries(kind.columns.map((c) => [c.id, c.value(obj, NOW)]));
}

/** An object that has only metadata, like one that is still being created: nothing may throw. */
export function expectToleratesMissingFields(kind: KindDescriptor): void {
  const bare = { metadata: { name: 'x', creationTimestamp: created } };
  expect(() => row(kind, bare)).not.toThrow();
  if (kind.status) expect(() => kind.status?.(bare)).not.toThrow();
  const empty = {};
  expect(() => row(kind, empty)).not.toThrow();
}

export function meta(name: string, namespace?: string, extra: Record<string, unknown> = {}) {
  return { name, ...(namespace ? { namespace } : {}), creationTimestamp: created, ...extra };
}
