import { Field } from '../../components/Field';
import type { ChartRef, HelmRepository } from './api';

export const OCI = 'oci';

export interface ChartInput {
  /** A repository name, `oci`, or '' while nothing is chosen. */
  source: string;
  chart: string;
  version: string;
  oci: string;
}

/** The chart reference the API takes, or undefined while the input is incomplete. */
export function chartRef(c: ChartInput): ChartRef | undefined {
  if (c.source === OCI)
    return c.oci === '' ? undefined : { oci: c.oci, ...(c.version && { version: c.version }) };
  if (c.source === '' || c.chart === '') return undefined;
  return { repo: c.source, name: c.chart, ...(c.version && { version: c.version }) };
}

/** Where the chart comes from: a repository and a chart name, or an OCI reference; and the version. */
export function ChartFields({
  value,
  onChange,
  repositories,
}: {
  value: ChartInput;
  onChange: (v: ChartInput) => void;
  repositories: HelmRepository[];
}) {
  const set = (k: keyof ChartInput) => (e: { target: { value: string } }) =>
    onChange({ ...value, [k]: e.target.value });
  return (
    <>
      <div className="field">
        <label htmlFor="helm-source">Repository</label>
        <select id="helm-source" value={value.source} onChange={set('source')}>
          <option value="">Choose…</option>
          {repositories.map((r) => (
            <option key={r.name} value={r.name}>
              {r.name}
            </option>
          ))}
          <option value={OCI}>OCI reference</option>
        </select>
      </div>
      {value.source === OCI ? (
        <Field
          label="OCI reference"
          value={value.oci}
          onChange={set('oci')}
          hint="For example oci://registry.example.com/charts/web"
        />
      ) : (
        <Field label="Chart" value={value.chart} onChange={set('chart')} />
      )}
      <Field
        label="Chart version"
        value={value.version}
        onChange={set('version')}
        hint="Empty means the newest release."
      />
    </>
  );
}
