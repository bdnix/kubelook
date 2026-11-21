import { stringify } from 'yaml';
import type { K8sObject } from '../../state/live';

/** Fields nested deeper than this are shown as YAML. */
const MAX_DEPTH = 8;

interface SchemaNode {
  description?: string;
  properties?: Record<string, SchemaNode>;
  items?: SchemaNode;
  additionalProperties?: unknown;
}

function node(v: unknown): SchemaNode | undefined {
  return v !== null && typeof v === 'object' ? (v as SchemaNode) : undefined;
}

function child(schema: SchemaNode | undefined, key: string | number): SchemaNode | undefined {
  if (!schema) return undefined;
  if (typeof key === 'number') return node(schema.items);
  return node(schema.properties?.[key]) ?? node(schema.additionalProperties);
}

function isMap(v: unknown): v is Record<string, unknown> {
  return v !== null && typeof v === 'object' && !Array.isArray(v);
}

function Value({ value, schema, depth }: { value: unknown; schema: SchemaNode | undefined; depth: number }) {
  if (depth >= MAX_DEPTH && (isMap(value) || Array.isArray(value))) {
    return <pre>{stringify(value, { lineWidth: 0 })}</pre>;
  }
  if (isMap(value)) {
    return (
      <ul>
        {Object.keys(value)
          .sort()
          .map((k) => (
            <Entry key={k} name={k} value={value[k]} schema={child(schema, k)} depth={depth + 1} />
          ))}
      </ul>
    );
  }
  if (Array.isArray(value)) {
    return (
      <ul>
        {value.map((v, i) => (
          <Entry key={i} name={String(i)} value={v} schema={child(schema, i)} depth={depth + 1} hideName />
        ))}
      </ul>
    );
  }
  return <span>{value === null || value === undefined ? '' : String(value)}</span>;
}

function Entry({
  name,
  value,
  schema,
  depth,
  hideName = false,
}: {
  name: string;
  value: unknown;
  schema: SchemaNode | undefined;
  depth: number;
  hideName?: boolean;
}) {
  return (
    <li>
      {!hideName && <strong>{name}</strong>}
      {!hideName && ' '}
      <Value value={value} schema={schema} depth={depth} />
      {schema?.description && <small className="field-description">{schema.description}</small>}
    </li>
  );
}

function Part({ title, value, schema }: { title: string; value: unknown; schema: SchemaNode | undefined }) {
  if (value === undefined || value === null) return null;
  return (
    <section aria-label={title}>
      <h3>{title}</h3>
      <Value value={value} schema={schema} depth={0} />
    </section>
  );
}

/**
 * The `spec` and `status` of a custom resource as a field tree. Descriptions come from the CRD's OpenAPI schema
 * (the root of one version, when the user may read it). `status.conditions` is left to the conditions table.
 */
export function SchemaDetails({ schema, object }: { schema: unknown; object: K8sObject }) {
  const root = node(schema);
  const status = isMap(object.status)
    ? Object.fromEntries(Object.entries(object.status).filter(([k]) => k !== 'conditions'))
    : object.status;
  const hasStatus = isMap(status) ? Object.keys(status).length > 0 : status !== undefined && status !== null;
  return (
    <>
      <Part title="Spec" value={object.spec} schema={child(root, 'spec')} />
      {hasStatus && <Part title="Status" value={status} schema={child(root, 'status')} />}
    </>
  );
}
