import { formatAge } from '../lib/age';
import { evaluateJsonPath } from '../lib/jsonpath';
import { ageColumn, nameColumn, namespaceColumn } from './columns';
import type { Column, KindDescriptor } from './types';

interface PrinterColumn {
  name: string;
  type?: string;
  jsonPath?: string;
}

/** The parts of a CustomResourceDefinition that the browser reads. */
export interface CrdLike {
  spec?: {
    group?: string;
    names?: { kind?: string; plural?: string };
    scope?: string;
    versions?: {
      name: string;
      served?: boolean;
      additionalPrinterColumns?: PrinterColumn[];
      schema?: { openAPIV3Schema?: unknown };
    }[];
  };
}

export interface KindIdentity {
  group: string;
  kind: string;
  resource: string;
  namespaced: boolean;
}

/** The name of the CRD object: `<plural>.<group>`. */
export function crdName(group: string, plural: string): string {
  return `${plural}.${group}`;
}

function printerColumn(c: PrinterColumn & { jsonPath: string }): Column {
  const path = c.jsonPath;
  if (c.type === 'integer' || c.type === 'number') {
    return {
      id: c.name,
      title: c.name,
      value: (o) => {
        const n = Number(evaluateJsonPath(o, path));
        return Number.isFinite(n) ? n : 0;
      },
      sort: 'number',
    };
  }
  if (c.type === 'date') {
    return {
      id: c.name,
      title: c.name,
      value: (o, now) => formatAge(evaluateJsonPath(o, path), now),
      sort: 'age',
    };
  }
  return { id: c.name, title: c.name, value: (o) => evaluateJsonPath(o, path) };
}

/**
 * The descriptor of a custom kind: Name, Namespace (when namespaced), the `additionalPrinterColumns` of the served
 * version, then Age. Without the CRD (the user may not read CRDs) only Name, Namespace and Age remain.
 */
export function crdDescriptor(
  crd: CrdLike | undefined,
  version: string,
  identity?: KindIdentity,
): KindDescriptor {
  const spec = crd?.spec;
  const id: KindIdentity = {
    group: spec?.group ?? identity?.group ?? '',
    kind: spec?.names?.kind ?? identity?.kind ?? '',
    resource: spec?.names?.plural ?? identity?.resource ?? '',
    namespaced: spec ? spec.scope !== 'Cluster' : (identity?.namespaced ?? true),
  };
  const printer = spec?.versions?.find((v) => v.name === version)?.additionalPrinterColumns ?? [];
  const columns = printer
    .filter(
      (c): c is PrinterColumn & { jsonPath: string } => typeof c.jsonPath === 'string' && c.jsonPath !== '',
    )
    .map(printerColumn);
  return {
    ...id,
    title: id.kind,
    section: 'Custom Resources',
    columns: [nameColumn, ...(id.namespaced ? [namespaceColumn] : []), ...columns, ageColumn],
  };
}

/** The OpenAPI schema of one version of a CRD, when the CRD has one. */
export function crdSchema(crd: CrdLike | undefined, version: string): unknown {
  return crd?.spec?.versions?.find((v) => v.name === version)?.schema?.openAPIV3Schema;
}
