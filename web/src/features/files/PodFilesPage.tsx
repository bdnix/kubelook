import { useState } from 'react';
import { getString } from '../../lib/path';
import { useKindItems } from '../details/useDetails';
import { ContainerPicker, runningContainers } from '../shell/ContainerPicker';
import { FilesPanel } from './FilesPanel';
import type { SaveFile } from '../../lib/saveFile';

/** The Pod Files page: pick a namespace, a pod and a container, then browse its files. */
export function PodFilesPage({ cluster, saveFile }: { cluster: string; saveFile?: SaveFile }) {
  const namespaces = useKindItems(cluster, '', 'Namespace', '', true);
  const [namespace, setNamespace] = useState('');
  const [podName, setPodName] = useState('');
  const [containerName, setContainerName] = useState('');
  const pods = useKindItems(cluster, '', 'Pod', namespace, namespace !== '');
  const pod = pods.find((p) => getString(p, 'metadata.name') === podName);
  const containers = pod ? runningContainers(pod) : [];
  const container = containers.some((c) => c.name === containerName)
    ? containerName
    : (containers[0]?.name ?? '');
  const names = (items: Record<string, unknown>[]) => items.map((i) => getString(i, 'metadata.name')).sort();

  return (
    <div>
      <h1>Pod Files</h1>
      <label>
        Namespace
        <select
          value={namespace}
          onChange={(e) => {
            setNamespace(e.target.value);
            setPodName('');
          }}
        >
          <option value="">Choose a namespace</option>
          {names(namespaces).map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
        </select>
      </label>
      <label>
        Pod
        <select value={podName} onChange={(e) => setPodName(e.target.value)} disabled={namespace === ''}>
          <option value="">Choose a pod</option>
          {names(pods).map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
        </select>
      </label>
      {containers.length > 0 && (
        <ContainerPicker containers={containers} value={container} onChange={setContainerName} />
      )}
      {pod && container !== '' && (
        <FilesPanel target={{ cluster, namespace, pod: podName, container }} saveFile={saveFile} />
      )}
    </div>
  );
}
