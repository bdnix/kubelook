import { Link, useParams } from 'react-router';
import { KarpenterPage } from '../karpenter/KarpenterPage';
import { clusterPath } from '../../lib/paths';
import { ModuleGate } from './ModuleGate';
import { MODULE_ENTRIES } from './registry';
import { useModules } from './useModules';

/** The page of one module. Until a module has views of its own it links to the lists of the kinds it claims. */
export function ModulePage({ cluster }: { cluster: string }) {
  const { name = '' } = useParams();
  const entry = MODULE_ENTRIES.find((e) => e.name === name);
  const modules = useModules(cluster);
  const kinds = modules?.find((m) => m.name === name)?.kinds ?? [];
  return (
    <ModuleGate
      cluster={cluster}
      name={entry ? name : ''}
      fallback={<p>This cluster does not have this module.</p>}
    >
      <h1>{entry?.title}</h1>
      {name === 'karpenter' && <KarpenterPage cluster={cluster} />}
      <ul>
        {kinds.map((k) => (
          <li key={`${k.group}/${k.kind}`}>
            <Link to={`${clusterPath(cluster)}/${k.group || 'core'}/${k.kind}`}>{k.kind}</Link>
          </li>
        ))}
      </ul>
    </ModuleGate>
  );
}
