import { quote } from '../../features/editor/kubectlCommand';
import { isDuration, parseDuration } from './duration';
import type { Filter, Term } from './parse';

export interface KubectlTarget {
  /** The resource name for kubectl, such as `pods` or `deployments.apps`. */
  resource: string;
  /** A namespace; the empty string for all namespaces; null for a cluster-scoped kind. */
  namespace: string | null;
}

const lit = (s: string) => JSON.stringify(s);

/** A path as a jq generator: `.spec.containers[]?.image`. */
function jqPath(path: string): string {
  let out = '';
  for (const part of path.split('.')) {
    const m = /^([^[]*)((?:\[[^\]]*\])*)$/.exec(part);
    if (!m) continue;
    const key = m[1] ?? '';
    if (key) out += /^[A-Za-z_][A-Za-z0-9_]*$/.test(key) ? `.${key}` : `.${lit(key)}`;
    for (const b of (m[2] ?? '').matchAll(/\[([^\]]*)\]/g)) out += b[1] === '*' ? '[]?' : `[${b[1]}]?`;
  }
  return out === '' ? '.' : out;
}

const SCALAR = '(type | IN("string","number","boolean"))';

function condition(t: Term): string {
  const value = t.value ?? '';
  switch (t.op) {
    case '=':
      return `. != null and tostring == ${lit(value)}`;
    case '!=':
      return `tostring != ${lit(value)}`;
    case 'contains':
      return `${SCALAR} and (tostring | contains(${lit(value)}))`;
    case 'regex':
      return `${SCALAR} and (tostring | test(${lit(value)}))`;
    case 'in':
      return `${SCALAR} and (tostring | IN(${(t.values ?? []).map(lit).join(',')}))`;
    case 'exists':
      return '. != null';
    default: {
      const op = t.op === 'older than' ? '<' : '>';
      const bound = isDuration(value)
        ? `(now - ${parseDuration(value) / 1000})`
        : `(${lit(value)} | fromdateiso8601)`;
      return `type == "string" and (try (fromdateiso8601 ${op} ${bound}) catch false)`;
    }
  }
}

const term = (t: Term) => `any(${jqPath(t.path)}; ${condition(t)})`;

function expression(filter: Filter): string {
  const groups = filter.map((g) => g.map(term).join(' and '));
  if (groups.length === 1) return groups[0] as string;
  return filter.map((g, i) => (g.length > 1 ? `(${groups[i]})` : groups[i])).join(' or ');
}

const shell = (s: string) => `'${s.replace(/'/g, `'\\''`)}'`;

/** The `kubectl get … | jq` (or `yq`) command that lists the objects the filter keeps. */
export function toKubectl(filter: Filter, target: KubectlTarget, tool: 'jq' | 'yq'): string {
  const ns =
    target.namespace === null ? '' : target.namespace === '' ? ' -A' : ` -n ${quote(target.namespace)}`;
  const select = filter.length === 0 ? '' : ` | select(${expression(filter)})`;
  return `kubectl get ${quote(target.resource)}${ns} -o ${tool === 'jq' ? 'json' : 'yaml'} | ${tool} ${shell(`.items[]${select}`)}`;
}
