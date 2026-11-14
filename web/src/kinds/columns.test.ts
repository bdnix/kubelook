import { describe, expect, it } from 'vitest';
import {
  ageColumn,
  conditionStatus,
  countColumn,
  countOf,
  defineKind,
  labelString,
  nameColumn,
  namespaceColumn,
  numberColumn,
  pathColumn,
} from './columns';

const now = Date.parse('2026-11-14T12:00:00Z');
const obj = {
  metadata: { name: 'a', namespace: 'ns', creationTimestamp: '2026-11-14T11:00:00Z' },
  spec: { n: 3, flag: true, text: 'x', list: [1, 2], map: { a: 1, b: 2 } },
  status: { conditions: [{ type: 'Ready', status: 'True' }] },
};

describe('column builders', () => {
  it('read name, namespace and age', () => {
    expect(nameColumn.value(obj, now)).toBe('a');
    expect(namespaceColumn.value(obj, now)).toBe('ns');
    expect(ageColumn.value(obj, now)).toBe('1h');
  });

  it('pathColumn shows strings, numbers and booleans as text and anything else as empty', () => {
    expect(pathColumn('x', 'X', 'spec.text').value(obj, now)).toBe('x');
    expect(pathColumn('x', 'X', 'spec.n').value(obj, now)).toBe('3');
    expect(pathColumn('x', 'X', 'spec.flag').value(obj, now)).toBe('true');
    expect(pathColumn('x', 'X', 'spec.list').value(obj, now)).toBe('');
    expect(pathColumn('x', 'X', 'spec.nope').value(obj, now)).toBe('');
  });

  it('numberColumn sorts numerically and defaults to 0', () => {
    const c = numberColumn('n', 'N', 'spec.n');
    expect(c.value(obj, now)).toBe(3);
    expect(c.sort).toBe('number');
    expect(numberColumn('n', 'N', 'spec.text').value(obj, now)).toBe(0);
  });

  it('countOf counts arrays and object keys', () => {
    expect(countOf(obj, 'spec.list')).toBe(2);
    expect(countOf(obj, 'spec.map')).toBe(2);
    expect(countOf(obj, 'spec.text')).toBe(0);
    expect(countOf(obj, 'spec.nope')).toBe(0);
    expect(countColumn('c', 'C', 'spec.list').value(obj, now)).toBe(2);
  });

  it('conditionStatus finds the condition by type', () => {
    expect(conditionStatus('Ready')(obj)).toBe('True');
    expect(conditionStatus('Other')(obj)).toBe('');
    expect(conditionStatus('Ready')({ metadata: {} })).toBe('');
  });

  it('labelString sorts pairs', () => {
    expect(labelString({ b: '2', a: '1' })).toBe('a=1,b=2');
    expect(labelString(undefined)).toBe('');
    expect(labelString('x')).toBe('');
  });

  it('defineKind puts Name and Namespace first and Age last, and skips Namespace for cluster kinds', () => {
    const base = { group: '', kind: 'K', resource: 'ks', title: 'Ks', section: 'S' };
    const namespaced = defineKind({
      ...base,
      namespaced: true,
      columns: [pathColumn('x', 'X', 'spec.text')],
    });
    expect(namespaced.columns.map((c) => c.id)).toEqual(['name', 'namespace', 'x', 'age']);
    const cluster = defineKind({ ...base, namespaced: false, columns: [] });
    expect(cluster.columns.map((c) => c.id)).toEqual(['name', 'age']);
  });
});
