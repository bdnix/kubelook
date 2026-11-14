export type Labels = Record<string, string>;

export interface Requirement {
  key: string;
  operator: 'In' | 'NotIn' | 'Exists' | 'DoesNotExist';
  values?: string[];
}

export interface LabelSelector {
  matchLabels?: Labels;
  matchExpressions?: Requirement[];
}

/** Every pair of `selector` is in `labels`. An empty selector matches everything. */
export function matchLabels(selector: Labels | undefined, labels: Labels | undefined): boolean {
  return Object.entries(selector ?? {}).every(([k, v]) => labels?.[k] === v);
}

/** Every requirement holds for `labels`. Unknown operators never match. */
export function matchExpressions(reqs: Requirement[] | undefined, labels: Labels | undefined): boolean {
  return (reqs ?? []).every((r) => {
    const value = labels?.[r.key];
    switch (r.operator) {
      case 'In':
        return value !== undefined && (r.values ?? []).includes(value);
      case 'NotIn':
        return value === undefined || !(r.values ?? []).includes(value);
      case 'Exists':
        return value !== undefined;
      case 'DoesNotExist':
        return value === undefined;
      default:
        return false;
    }
  });
}

/** A Kubernetes label selector against labels. An empty selector matches everything. */
export function matchSelector(selector: LabelSelector | undefined, labels: Labels | undefined): boolean {
  return matchLabels(selector?.matchLabels, labels) && matchExpressions(selector?.matchExpressions, labels);
}
