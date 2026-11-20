import { describe, expect, it } from 'vitest';
import { getAt, removeAt, schemaToFields, setAt, type Field } from './schemaToFields';

const kinds = (fields: Field[]) => fields.map((f) => `${f.path.join('.')}:${f.kind}`);

describe('schemaToFields', () => {
  it('maps scalars, sorts by name and marks required fields', () => {
    const fields = schemaToFields({
      type: 'object',
      required: ['size'],
      properties: {
        zone: { type: 'string', description: 'where' },
        size: { type: 'integer' },
        ratio: { type: 'number' },
        on: { type: 'boolean' },
      },
    });
    expect(kinds(fields)).toEqual(['on:boolean', 'ratio:number', 'size:integer', 'zone:string']);
    expect(fields.find((f) => f.path[0] === 'size')?.required).toBe(true);
    expect(fields.find((f) => f.path[0] === 'zone')?.required).toBe(false);
    expect(fields.find((f) => f.path[0] === 'zone')?.description).toBe('where');
  });

  it('turns a string with an enum into a select', () => {
    const [f] = schemaToFields({
      type: 'object',
      properties: { mode: { type: 'string', enum: ['a', 'b'] } },
    });
    expect(f).toMatchObject({ kind: 'enum', options: ['a', 'b'] });
  });

  it('nests objects as groups', () => {
    const [g] = schemaToFields({
      type: 'object',
      properties: { db: { type: 'object', required: ['host'], properties: { host: { type: 'string' } } } },
    });
    expect(g).toMatchObject({ kind: 'group', path: ['db'] });
    expect((g as Extract<Field, { kind: 'group' }>).children[0]).toMatchObject({
      kind: 'string',
      path: ['db', 'host'],
      required: true,
    });
  });

  it('turns an array of scalars into a list', () => {
    const [f] = schemaToFields({
      type: 'object',
      properties: { tags: { type: 'array', items: { type: 'string' } } },
    });
    expect(f).toMatchObject({ kind: 'list', item: { kind: 'string' } });
  });

  it('turns an array of objects into a list of groups while it is shallow', () => {
    const [f] = schemaToFields({
      type: 'object',
      properties: {
        ports: { type: 'array', items: { type: 'object', properties: { port: { type: 'integer' } } } },
      },
    });
    expect(f).toMatchObject({ kind: 'list', item: { kind: 'group' } });
  });

  it('turns an array of objects deeper than 2 levels into YAML', () => {
    const [outer] = schemaToFields({
      type: 'object',
      properties: {
        a: {
          type: 'object',
          properties: {
            b: {
              type: 'object',
              properties: {
                c: { type: 'array', items: { type: 'object', properties: { d: { type: 'string' } } } },
              },
            },
          },
        },
      },
    });
    const b = (outer as Extract<Field, { kind: 'group' }>).children[0] as Extract<Field, { kind: 'group' }>;
    expect(b.children[0]).toMatchObject({ kind: 'yaml', path: ['a', 'b', 'c'] });
  });

  it.each([
    ['oneOf', { oneOf: [{ type: 'string' }, { type: 'integer' }] }],
    ['anyOf', { anyOf: [{ type: 'string' }] }],
    ['preserve unknown fields', { type: 'object', 'x-kubernetes-preserve-unknown-fields': true }],
    ['additionalProperties', { type: 'object', additionalProperties: { type: 'string' } }],
    ['int or string', { 'x-kubernetes-int-or-string': true }],
    ['an object without properties', { type: 'object' }],
    ['no type', {}],
    ['an array without items', { type: 'array' }],
    ['an array of arrays', { type: 'array', items: { type: 'array', items: { type: 'string' } } }],
  ])('%s becomes an embedded YAML field', (_name, schema) => {
    const [f] = schemaToFields({ type: 'object', properties: { x: schema } });
    expect(f).toMatchObject({ kind: 'yaml', path: ['x'] });
  });

  it('falls back to YAML below the depth cap', () => {
    let schema: Record<string, unknown> = { type: 'string' };
    for (let i = 0; i < 12; i++) schema = { type: 'object', properties: { n: schema } };
    let f: Field | undefined = schemaToFields(schema)[0];
    let depth = 1;
    while (f && f.kind === 'group') {
      f = f.children[0];
      depth++;
    }
    expect(f?.kind).toBe('yaml');
    expect(depth).toBeLessThanOrEqual(10);
  });

  it('returns no fields for a schema that is not an object', () => {
    expect(schemaToFields(undefined)).toEqual([]);
    expect(schemaToFields({ type: 'string' })).toEqual([]);
  });
});

describe('getAt, setAt and removeAt', () => {
  it('reads a path', () => {
    expect(getAt({ a: { b: 1 } }, ['a', 'b'])).toBe(1);
    expect(getAt({ a: 1 }, ['a', 'b'])).toBeUndefined();
  });

  it('sets a path without changing the original and creates the objects on the way', () => {
    const before = { a: { x: 1 } };
    const after = setAt(before, ['a', 'b', 'c'], 2);
    expect(after).toEqual({ a: { x: 1, b: { c: 2 } } });
    expect(before).toEqual({ a: { x: 1 } });
  });

  it('removes a path and the objects it leaves empty', () => {
    expect(removeAt({ a: { b: { c: 1 } }, k: 1 }, ['a', 'b', 'c'])).toEqual({ k: 1 });
    expect(removeAt({ a: { b: 1, c: 2 } }, ['a', 'b'])).toEqual({ a: { c: 2 } });
    expect(removeAt({ a: 1 }, ['x', 'y'])).toEqual({ a: 1 });
  });
});
