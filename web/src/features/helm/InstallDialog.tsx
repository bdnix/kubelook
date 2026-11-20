import { useRef, useState } from 'react';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { Field } from '../../components/Field';
import { useFocusTrap } from '../../lib/useFocusTrap';
import { YamlEditor } from '../editor/lazyEditors';
import { useHelmJob, useRepositories } from './api';
import { ChartFields, chartRef, type ChartInput } from './ChartFields';
import { parseValues } from './values';

/** Installs a chart from a repository or an OCI reference as a job. */
export function InstallDialog({ cluster, onClose }: { cluster: string; onClose: () => void }) {
  const repos = useRepositories(cluster);
  const job = useHelmJob(cluster);
  const ref = useRef<HTMLDivElement>(null);
  useFocusTrap(ref);
  const [chart, setChart] = useState<ChartInput>({ source: '', chart: '', version: '', oci: '' });
  const [namespace, setNamespace] = useState('');
  const [name, setName] = useState('');
  const [text, setText] = useState('');
  const values = parseValues(text);
  const reference = chartRef(chart);
  const ready = reference !== undefined && namespace !== '' && name !== '' && values.ok;
  return (
    <div
      ref={ref}
      role="dialog"
      aria-modal="true"
      aria-label="Install a chart"
      tabIndex={-1}
      onKeyDown={(e) => e.key === 'Escape' && onClose()}
    >
      <h2>Install a chart</h2>
      <ChartFields value={chart} onChange={setChart} repositories={repos.data ?? []} />
      <Field label="Namespace" value={namespace} onChange={(e) => setNamespace(e.target.value)} />
      <Field label="Release name" value={name} onChange={(e) => setName(e.target.value)} />
      <YamlEditor label="Values (YAML)" value={text} onChange={setText} />
      <ErrorText message={values.ok ? job.error : values.error} />
      <Button onClick={onClose}>Cancel</Button>
      <Button
        disabled={!ready || job.busy}
        onClick={() => {
          if (!reference || !values.ok) return;
          void job
            .start({ action: 'install', namespace, name, chart: reference, values: values.value })
            .then((ok) => ok && onClose());
        }}
      >
        Install
      </Button>
    </div>
  );
}
