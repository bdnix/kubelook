/** A node of an OpenAPI schema, as far as the form reads it. */
interface Schema {
  type?: string;
  description?: string;
  enum?: unknown[];
  required?: string[];
  properties?: Record<string, Schema>;
  items?: Schema;
  additionalProperties?: unknown;
  oneOf?: unknown;
  anyOf?: unknown;
  allOf?: unknown;
  'x-kubernetes-preserve-unknown-fields'?: boolean;
  'x-kubernetes-int-or-string'?: boolean;
}

interface Base {
  /** Where the value lives inside the object being created. */
  path: string[];
  label: string;
  description?: string;
  required: boolean;
}

export type ScalarField = Base & { kind: 'string' | 'integer' | 'number' | 'boolean' };
export type EnumField = Base & { kind: 'enum'; options: string[] };
export type GroupField = Base & { kind: 'group'; children: Field[] };
export type ListField = Base & { kind: 'list'; item: ScalarField | EnumField | GroupField };
/** A node the form cannot draw; it is edited as YAML inside the form. */
export type YamlField = Base & { kind: 'yaml' };
export type Field = ScalarField | EnumField | GroupField | ListField | YamlField;

/** Groups nested deeper than this are edited as YAML. */
export const MAX_DEPTH = 8;
/** Lists of objects are drawn only while they are at most this deep inside other groups and lists. */
const MAX_LIST_OF_OBJECTS_DEPTH = 2;

const SCALARS = ['string', 'integer', 'number', 'boolean'] as const;

function isScalar(t: string | undefined): t is (typeof SCALARS)[number] {
  return SCALARS.includes(t as (typeof SCALARS)[number]);
}

function cannotDraw(s: Schema): boolean {
  return (
    s.oneOf !== undefined ||
    s.anyOf !== undefined ||
    s.allOf !== undefined ||
    s['x-kubernetes-preserve-unknown-fields'] === true ||
    s['x-kubernetes-int-or-string'] === true ||
    s.additionalProperties !== undefined
  );
}

function scalar(s: Schema, base: Base): ScalarField | EnumField | undefined {
  if (!isScalar(s.type)) return undefined;
  if (s.type === 'string' && Array.isArray(s.enum) && s.enum.every((e) => typeof e === 'string')) {
    return { ...base, kind: 'enum', options: s.enum as string[] };
  }
  return { ...base, kind: s.type };
}

function group(s: Schema, base: Base, depth: number): GroupField {
  return { ...base, kind: 'group', children: fieldsOf(s, base.path, depth + 1) };
}

function fieldFor(s: Schema, base: Base, depth: number): Field {
  const asYaml: YamlField = { ...base, kind: 'yaml' };
  if (cannotDraw(s)) return asYaml;
  const flat = scalar(s, base);
  if (flat) return flat;
  const type = s.type ?? (s.properties ? 'object' : undefined);
  if (type === 'object') {
    return s.properties && Object.keys(s.properties).length > 0 && depth < MAX_DEPTH
      ? group(s, base, depth)
      : asYaml;
  }
  if (type === 'array' && s.items) {
    const item = scalar(s.items, base);
    if (item) return { ...base, kind: 'list', item };
    if (
      depth < MAX_LIST_OF_OBJECTS_DEPTH &&
      !cannotDraw(s.items) &&
      s.items.properties &&
      Object.keys(s.items.properties).length > 0
    ) {
      return { ...base, kind: 'list', item: group(s.items, base, depth) };
    }
  }
  return asYaml;
}

function fieldsOf(s: Schema, parent: string[], depth: number): Field[] {
  const required = new Set(s.required ?? []);
  const properties = s.properties ?? {};
  return Object.keys(properties)
    .sort()
    .map((name) => {
      const child = properties[name] as Schema;
      const base: Base = { path: [...parent, name], label: name, required: required.has(name) };
      if (child.description !== undefined) base.description = child.description;
      return fieldFor(child, base, depth);
    });
}

/** The form fields of the properties of an object schema (a CRD's `spec`, for instance). */
export function schemaToFields(schema: unknown): Field[] {
  const s = schema as Schema | undefined;
  if (!s || typeof s !== 'object' || s.type !== 'object') return [];
  return fieldsOf(s, [], 0);
}

export function getAt(obj: unknown, path: string[]): unknown {
  let cur = obj;
  for (const key of path) {
    if (cur === null || typeof cur !== 'object') return undefined;
    cur = (cur as Record<string, unknown>)[key];
  }
  return cur;
}

/** A copy of `obj` with `value` at `path`; objects on the way are created. */
export function setAt(obj: Record<string, unknown>, path: string[], value: unknown): Record<string, unknown> {
  const [head, ...rest] = path;
  if (head === undefined) return obj;
  if (rest.length === 0) return { ...obj, [head]: value };
  const next = obj[head];
  const inner =
    next !== null && typeof next === 'object' && !Array.isArray(next)
      ? (next as Record<string, unknown>)
      : {};
  return { ...obj, [head]: setAt(inner, rest, value) };
}

/** A copy of `obj` without `path`; objects that become empty are removed too. */
export function removeAt(obj: Record<string, unknown>, path: string[]): Record<string, unknown> {
  const [head, ...rest] = path;
  if (head === undefined || !(head in obj)) return obj;
  const { [head]: current, ...others } = obj;
  if (rest.length === 0) return others;
  if (current === null || typeof current !== 'object' || Array.isArray(current)) return obj;
  const inner = removeAt(current as Record<string, unknown>, rest);
  return Object.keys(inner).length === 0 ? others : { ...others, [head]: inner };
}
