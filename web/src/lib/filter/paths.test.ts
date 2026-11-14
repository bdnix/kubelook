import { describe, expect, it } from 'vitest';
import { collectPaths, SECRET_MESSAGE, secretViolation } from './paths';
import { parseFilter, type Filter } from './parse';

const f = (text: string): Filter => {
  const r = parseFilter(text);
  if (!r.ok) throw new Error(r.error.message);
  return r.filter;
};

describe('collectPaths', () => {
  it('collects the paths of objects, with arrays as [*]', () => {
    const paths = collectPaths(
      [
        {
          metadata: { name: 'a', labels: { app: 'web' } },
          spec: { containers: [{ name: 'x', image: 'i', ports: [{ containerPort: 80 }] }], args: ['--v'] },
        },
      ],
      { group: '', kind: 'Pod' },
    );
    expect(paths).toEqual([
      'metadata',
      'metadata.labels',
      'metadata.labels.app',
      'metadata.name',
      'spec',
      'spec.args',
      'spec.args[*]',
      'spec.containers',
      'spec.containers[*].image',
      'spec.containers[*].name',
      'spec.containers[*].ports',
      'spec.containers[*].ports[*].containerPort',
    ]);
  });

  it('merges the paths of the objects, sorted and without duplicates', () => {
    const paths = collectPaths(
      [
        { b: 1, a: { x: 1 } },
        { b: 2, c: 1 },
      ],
      { group: '', kind: 'Pod' },
    );
    expect(paths).toEqual(['a', 'a.x', 'b', 'c']);
  });

  it('looks at the first 50 objects only', () => {
    const objects = Array.from({ length: 60 }, (_, i) => ({ [`k${i}`]: 1 }));
    const paths = collectPaths(objects, { group: '', kind: 'Pod' });
    expect(paths).toContain('k49');
    expect(paths).not.toContain('k50');
  });

  it('stops at depth 6', () => {
    const deep = { a: { b: { c: { d: { e: { f: { g: 1 } } } } } } };
    const paths = collectPaths([deep], { group: '', kind: 'Pod' });
    expect(paths).toContain('a.b.c.d.e.f');
    expect(paths).not.toContain('a.b.c.d.e.f.g');
  });

  it('leaves out null values and keys that cannot be written as a path', () => {
    const paths = collectPaths([{ a: null, 'b c': 1, 'd.e': 2, ok: 1 }], { group: '', kind: 'Pod' });
    expect(paths).toEqual(['a', 'ok']);
  });

  it('never suggests Secret data', () => {
    const secret = {
      metadata: { name: 's' },
      type: 'Opaque',
      data: { password: 'x' },
      stringData: { user: 'y' },
    };
    expect(collectPaths([secret], { group: '', kind: 'Secret' })).toEqual([
      'metadata',
      'metadata.name',
      'type',
    ]);
    expect(collectPaths([secret], { group: '', kind: 'ConfigMap' })).toContain('data.password');
  });
});

describe('secretViolation', () => {
  it('rejects a filter on the values of a Secret', () => {
    expect(secretViolation(f('data.password = x'), { group: '', kind: 'Secret' })).toBe(SECRET_MESSAGE);
    expect(secretViolation(f('stringData.user exists'), { group: '', kind: 'Secret' })).toBe(SECRET_MESSAGE);
    expect(secretViolation(f('metadata.name = a OR data.x contains y'), { group: '', kind: 'Secret' })).toBe(
      SECRET_MESSAGE,
    );
    expect(SECRET_MESSAGE).toBe("Secret values can't be filtered.");
  });

  it('allows everything else', () => {
    expect(
      secretViolation(f('type = Opaque AND metadata.name = a'), { group: '', kind: 'Secret' }),
    ).toBeUndefined();
    expect(secretViolation(f('data.x = y'), { group: '', kind: 'ConfigMap' })).toBeUndefined();
    expect(secretViolation(f('database.x = y'), { group: '', kind: 'Secret' })).toBeUndefined();
  });
});
