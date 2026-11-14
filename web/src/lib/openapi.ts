type Schemas = Record<
  string,
  { 'x-kubernetes-group-version-kind'?: { group?: string; version?: string; kind?: string }[] }
>;

/** The `$ref` of the schema of a kind inside an OpenAPI v3 document, or `undefined` when it has none. */
export function findSchemaRef(
  doc: unknown,
  group: string,
  version: string,
  kind: string,
): string | undefined {
  const schemas = (doc as { components?: { schemas?: Schemas } } | null)?.components?.schemas;
  if (!schemas) return undefined;
  const g = group === 'core' ? '' : group;
  for (const [name, schema] of Object.entries(schemas)) {
    const hit = schema['x-kubernetes-group-version-kind']?.some(
      (x) => (x.group ?? '') === g && x.version === version && x.kind === kind,
    );
    if (hit) return `#/components/schemas/${name}`;
  }
  return undefined;
}
