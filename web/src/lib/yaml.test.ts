import { describe, expect, it } from 'vitest';
import { parseObject, splitDocuments, toYaml } from './yaml';

const pod = {
  apiVersion: 'v1',
  kind: 'Pod',
  metadata: { name: 'p', resourceVersion: '7', managedFields: [{ manager: 'x' }] },
  spec: { containers: [{ name: 'a' }] },
  status: { phase: 'Running' },
};

describe('toYaml', () => {
  it('leaves out status and managedFields but keeps the resourceVersion', () => {
    const text = toYaml(pod);
    expect(text).not.toContain('status');
    expect(text).not.toContain('managedFields');
    expect(text).toContain('resourceVersion: "7"');
    expect(text).toContain('kind: Pod');
  });

  it('can keep the status', () => {
    expect(toYaml(pod, { withStatus: true })).toContain('phase: Running');
  });

  it('handles an object without metadata', () => {
    expect(toYaml({ kind: 'X' })).toBe('kind: X\n');
  });

  it('does not change its input', () => {
    toYaml(pod);
    expect(pod.status).toBeDefined();
    expect(pod.metadata.managedFields).toHaveLength(1);
  });
});

describe('parseObject', () => {
  it('parses YAML and JSON mappings', () => {
    expect(parseObject('kind: Pod\nmetadata:\n  name: p\n')).toEqual({
      ok: true,
      value: { kind: 'Pod', metadata: { name: 'p' } },
    });
    expect(parseObject('{"kind":"Pod"}')).toEqual({ ok: true, value: { kind: 'Pod' } });
  });

  it.each(['', '- a\n- b\n', 'just text', '42'])('%j is not an object', (text) => {
    const r = parseObject(text);
    expect(r.ok).toBe(false);
  });

  it('reports a syntax error as text', () => {
    const r = parseObject('a: [');
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error.length).toBeGreaterThan(0);
  });
});

describe('splitDocuments', () => {
  it('splits on --- and drops empty documents', () => {
    const docs = splitDocuments('a: 1\n---\n---\nb: 2\n---\n');
    expect(docs).toHaveLength(2);
    expect(docs[0]).toContain('a: 1');
    expect(docs[1]).toContain('b: 2');
    expect(splitDocuments('')).toEqual([]);
  });
});
