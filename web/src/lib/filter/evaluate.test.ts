import { describe, expect, it } from 'vitest';
import { compileFilter, resolvePath } from './evaluate';
import { parseFilter, type Filter } from './parse';

const NOW = Date.parse('2026-11-14T12:00:00Z');
const f = (text: string): Filter => {
  const r = parseFilter(text);
  if (!r.ok) throw new Error(r.error.message);
  return r.filter;
};
const test = (text: string, obj: unknown) => compileFilter(f(text), NOW).test(obj);

const pod = {
  metadata: {
    name: 'web-1',
    creationTimestamp: '2026-11-01T12:00:00Z',
    labels: { app: 'web' },
    deletionTimestamp: null,
  },
  spec: {
    replicas: 3,
    paused: false,
    containers: [
      { name: 'app', image: 'nginx:1.27' },
      { name: 'sidecar', image: 'envoy:1.30' },
    ],
  },
  status: { phase: 'Running', ratio: 0.5 },
};

describe('resolvePath', () => {
  it('follows keys, indexes and [*]', () => {
    expect(resolvePath(pod, 'status.phase')).toEqual(['Running']);
    expect(resolvePath(pod, 'spec.containers[1].name')).toEqual(['sidecar']);
    expect(resolvePath(pod, 'spec.containers[*].name')).toEqual(['app', 'sidecar']);
    expect(resolvePath(pod, 'spec.containers[*]')).toHaveLength(2);
  });

  it('a missing key is undefined, a missing array has no elements', () => {
    expect(resolvePath(pod, 'status.nope.deeper')).toEqual([undefined]);
    expect(resolvePath(pod, 'spec.nope[*].x')).toEqual([]);
    expect(resolvePath(pod, 'spec.containers[9].name')).toEqual([undefined]);
    expect(resolvePath(pod, 'status.phase[*]')).toEqual([]);
    expect(resolvePath(null, 'a.b')).toEqual([undefined]);
  });
});

describe('= and !=', () => {
  it('compares text, numbers and booleans', () => {
    expect(test('status.phase = Running', pod)).toBe(true);
    expect(test('status.phase = Pending', pod)).toBe(false);
    expect(test('spec.replicas = 3', pod)).toBe(true);
    expect(test('spec.replicas = 3.0', pod)).toBe(true);
    expect(test('spec.replicas = 4', pod)).toBe(false);
    expect(test('spec.paused = false', pod)).toBe(true);
    expect(test('spec.paused = true', pod)).toBe(false);
    expect(test('status.ratio = 0.5', pod)).toBe(true);
    expect(test('metadata.name != web-2', pod)).toBe(true);
    expect(test('metadata.name != web-1', pod)).toBe(false);
  });

  it('compares numbers numerically only when both sides are numbers', () => {
    expect(test('a = 10', { a: '10.0' })).toBe(true);
    expect(test('a = 1e2', { a: 100 })).toBe(true);
    expect(test('a = 010', { a: 10 })).toBe(true);
    expect(test('a = abc', { a: 'abc' })).toBe(true);
    expect(test('a = 1', { a: 'one' })).toBe(false);
    expect(test('a = ""', { a: '' })).toBe(true);
  });

  it('a missing field is not equal to anything, and is different from everything', () => {
    expect(test('nope = x', pod)).toBe(false);
    expect(test('nope != x', pod)).toBe(true);
    expect(test('metadata.deletionTimestamp = x', pod)).toBe(false);
  });

  it('[*] matches when any element matches', () => {
    expect(test('spec.containers[*].image = envoy:1.30', pod)).toBe(true);
    expect(test('spec.containers[*].image = redis', pod)).toBe(false);
    expect(test('spec.containers[*].image != envoy:1.30', pod)).toBe(true); // the other element differs
    expect(test('spec.nope[*].image != x', pod)).toBe(false); // no element at all
  });
});

describe('contains, regex, in, exists', () => {
  it('contains finds text inside scalars', () => {
    expect(test('metadata.name contains eb-', pod)).toBe(true);
    expect(test('metadata.name contains WEB', pod)).toBe(false);
    expect(test('spec.replicas contains 3', pod)).toBe(true);
    expect(test('spec.containers[*].image contains envoy', pod)).toBe(true);
    expect(test('spec.containers contains envoy', pod)).toBe(false);
    expect(test('nope contains x', pod)).toBe(false);
  });

  it('regex tests scalars', () => {
    expect(test('metadata.name regex ^web-\\d+$', pod)).toBe(true);
    expect(test('metadata.name regex ^api', pod)).toBe(false);
    expect(test('spec.containers[*].image regex ^envoy:', pod)).toBe(true);
  });

  it('an invalid regex filters nothing and is reported', () => {
    const c = compileFilter(f('metadata.name regex ('), NOW);
    expect(c.test(pod)).toBe(true);
    expect(c.test({})).toBe(true);
    expect(c.invalid).toEqual([{ path: 'metadata.name', op: 'regex', value: '(' }]);
    const mixed = compileFilter(f('metadata.name regex ( AND status.phase = Pending'), NOW);
    expect(mixed.test(pod)).toBe(false); // the other condition still applies
    expect(mixed.invalid).toHaveLength(1);
  });

  it('in matches any listed value', () => {
    expect(test('status.phase in (Pending, Running)', pod)).toBe(true);
    expect(test('status.phase in (Pending, Failed)', pod)).toBe(false);
    expect(test('spec.replicas in (1, 3)', pod)).toBe(true);
  });

  it('exists is true for present non-null values', () => {
    expect(test('metadata.name exists', pod)).toBe(true);
    expect(test('metadata.deletionTimestamp exists', pod)).toBe(false);
    expect(test('metadata.nope exists', pod)).toBe(false);
    expect(test('spec.paused exists', pod)).toBe(true);
    expect(test('spec.containers[*].name exists', pod)).toBe(true);
    expect(test('spec.nope[*].name exists', pod)).toBe(false);
  });
});

describe('older than and newer than', () => {
  it('use the injected now with a duration', () => {
    expect(test('metadata.creationTimestamp older than 7d', pod)).toBe(true); // 13 days old
    expect(test('metadata.creationTimestamp older than 2w', pod)).toBe(false);
    expect(test('metadata.creationTimestamp newer than 2w', pod)).toBe(true);
    expect(test('metadata.creationTimestamp newer than 7d', pod)).toBe(false);
    expect(
      test('metadata.creationTimestamp older than 1w', {
        metadata: { creationTimestamp: '2026-11-14T11:59:00Z' },
      }),
    ).toBe(false);
    expect(
      test('metadata.creationTimestamp newer than 1m', {
        metadata: { creationTimestamp: '2026-11-14T11:59:30Z' },
      }),
    ).toBe(true);
  });

  it('work against a timestamp', () => {
    expect(test('metadata.creationTimestamp older than 2026-11-10T00:00:00Z', pod)).toBe(true);
    expect(test('metadata.creationTimestamp older than 2026-10-01T00:00:00Z', pod)).toBe(false);
    expect(test('metadata.creationTimestamp newer than 2026-10-01T00:00:00Z', pod)).toBe(true);
  });

  it('a missing or unreadable time matches neither', () => {
    expect(test('metadata.nope older than 1d', pod)).toBe(false);
    expect(test('metadata.nope newer than 1d', pod)).toBe(false);
    expect(test('metadata.name older than 1d', pod)).toBe(false);
    expect(test('spec.replicas newer than 1d', pod)).toBe(false);
  });
});

describe('AND and OR', () => {
  it('combine with AND tighter than OR', () => {
    expect(test('status.phase = Pending OR metadata.name = web-1 AND spec.replicas = 3', pod)).toBe(true);
    expect(test('status.phase = Pending OR metadata.name = web-1 AND spec.replicas = 4', pod)).toBe(false);
    expect(test('status.phase = Running AND spec.replicas = 4', pod)).toBe(false);
  });

  it('an empty filter matches everything', () => {
    expect(compileFilter([], NOW).test(pod)).toBe(true);
  });
});
