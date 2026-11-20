import { describe, expect, it } from 'vitest';
import { evaluateJsonPath } from './jsonpath';

const obj = {
  metadata: { name: 'a', labels: { 'x.y': 'dotted', plain: 'p' } },
  spec: { list: ['first', 'second'], items: [{ b: 1 }, { b: 2 }, { c: 3 }], n: 4, ok: true },
  status: {
    conditions: [
      { type: 'Initialized', status: 'False' },
      { type: 'Ready', status: 'True' },
    ],
  },
};

describe('evaluateJsonPath', () => {
  it.each([
    ['.metadata.name', 'a'],
    ['.spec.list[0]', 'first'],
    ['.spec.list[1]', 'second'],
    ['.spec.list[5]', ''],
    ['.spec.items[*].b', '1,2'],
    ['.spec.list[*]', 'first,second'],
    ['.status.conditions[?(@.type=="Ready")].status', 'True'],
    ["status.conditions[?(@.type=='Initialized')].status", 'False'],
    ['.status.conditions[?(@.type=="Missing")].status', ''],
    ['.metadata.labels[\'x\\.y\']', 'dotted'],
    ['.metadata.labels.plain', 'p'],
    ['.spec.n', '4'],
    ['.spec.ok', 'true'],
    ['.spec.missing.deeper', ''],
  ])('%s -> %s', (path, expected) => {
    expect(evaluateJsonPath(obj, path)).toBe(expected);
  });

  it('shows an object value as JSON', () => {
    expect(evaluateJsonPath(obj, '.spec.items[0]')).toBe('{"b":1}');
  });

  it.each(['', '.spec[', '.spec.items[?(@.b>1)].b', '..spec', '.spec.items[1:2]', '{.spec}', '.a[?(', "['unterminated"])(
    'unsupported syntax %j gives an empty string instead of throwing',
    (path) => {
      expect(evaluateJsonPath(obj, path)).toBe('');
    },
  );
});
