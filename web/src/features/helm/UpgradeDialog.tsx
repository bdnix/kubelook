import { useEffect, useRef, useState } from 'react';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { diffLines } from '../../lib/diff';
import { useFocusTrap } from '../../lib/useFocusTrap';
import { stringify } from 'yaml';
import { YamlEditor } from '../editor/lazyEditors';
import { SavePreview } from '../editor/SavePreview';
import { useHelmJob, usePreview, useRepositories, useRevealValues, type HelmRelease } from './api';
import { ChartFields, chartRef, type ChartInput } from './ChartFields';
import { parseValues } from './values';

/**
 * Upgrades a release with edited values. The values start as the release's own (read through the audited
 * endpoint); Confirm is enabled only after a preview that shows the values diff and the manifest diff, and
 * only for exactly the inputs that were previewed.
 */
export function UpgradeDialog({
  cluster,
  release,
  onClose,
}: {
  cluster: string;
  release: HelmRelease;
  onClose: () => void;
}) {
  const repos = useRepositories(cluster);
  const reveal = useRevealValues(cluster, release.namespace, release.name);
  const preview = usePreview(cluster);
  const job = useHelmJob(cluster);
  const ref = useRef<HTMLDivElement>(null);
  useFocusTrap(ref);
  const [chart, setChart] = useState<ChartInput>({ source: '', chart: release.chart, version: '', oci: '' });
  const [original, setOriginal] = useState('');
  const [text, setText] = useState('');
  const [previewed, setPreviewed] = useState<string>();
  const { mutate: readValues } = reveal;
  useEffect(() => {
    readValues(release.revision, {
      onSuccess: (v) => {
        setOriginal(v.userValues);
        setText((current) => (current === '' ? v.userValues : current));
      },
    });
  }, [readValues, release.revision]);

  const values = parseValues(text);
  const reference = chartRef(chart);
  const key = JSON.stringify([chart, text]);
  const action = () =>
    reference && values.ok
      ? ({
          action: 'upgrade',
          namespace: release.namespace,
          name: release.name,
          chart: reference,
          values: values.value,
        } as const)
      : undefined;
  const body = action();
  const normalized = (t: string) => {
    const v = parseValues(t);
    return v.ok && Object.keys(v.value).length > 0 ? stringify(v.value, { lineWidth: 0 }) : '';
  };
  return (
    <div
      ref={ref}
      role="dialog"
      aria-modal="true"
      aria-label={`Upgrade ${release.name}`}
      tabIndex={-1}
      onKeyDown={(e) => e.key === 'Escape' && onClose()}
    >
      <h2>{`Upgrade ${release.name}`}</h2>
      <ChartFields value={chart} onChange={setChart} repositories={repos.data ?? []} />
      <YamlEditor label="Values (YAML)" value={text} onChange={setText} />
      <ErrorText
        message={values.ok ? (preview.error?.message ?? job.error ?? reveal.error?.message) : values.error}
      />
      <Button
        disabled={!body || preview.isPending}
        onClick={() => {
          if (body) preview.mutate(body, { onSuccess: () => setPreviewed(key) });
        }}
      >
        Preview
      </Button>
      {previewed === key && preview.data && values.ok && (
        <>
          <section aria-label="Values diff">
            <h3>Values</h3>
            <SavePreview diff={diffLines(normalized(original), normalized(text))} />
          </section>
          <section aria-label="Manifest diff">
            <h3>Manifest</h3>
            <SavePreview diff={diffLines(preview.data.currentManifest, preview.data.newManifest)} />
          </section>
        </>
      )}
      <Button onClick={onClose}>Cancel</Button>
      <Button
        disabled={previewed !== key || !body || job.busy}
        onClick={() => {
          if (body) void job.start(body).then((ok) => ok && onClose());
        }}
      >
        Confirm upgrade
      </Button>
    </div>
  );
}
