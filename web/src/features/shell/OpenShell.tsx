import { useRef, useState } from 'react';
import { useAccess } from '../../api/access';
import { Button } from '../../components/Button';
import { missingPermission } from '../../kinds/actions';
import { getString } from '../../lib/path';
import { shellTab, useDock } from '../../state/dock';
import type { K8sObject } from '../../state/live';
import { ContainerPicker, runningContainers } from './ContainerPicker';

/** Opens a shell of a pod in the dock: pick a running container, then open. Needs `create` on `pods/exec`. */
export function OpenShell({ cluster, pod }: { cluster: string; pod: K8sObject }) {
  const open = useDock().open;
  const serial = useRef(0);
  const containers = runningContainers(pod);
  const [chosen, setChosen] = useState<string>();
  const name = getString(pod, 'metadata.name');
  const namespace = getString(pod, 'metadata.namespace');
  const check = { verb: 'create', group: '', resource: 'pods', subresource: 'exec', namespace, name };
  const access = useAccess(cluster, [check]);
  const denied = access.data?.[0]?.allowed === false;
  const container = chosen ?? containers[0]?.name ?? '';
  if (containers.length === 0) return null;
  return (
    <div className="open-shell">
      <ContainerPicker containers={containers} value={container} onChange={setChosen} />
      <Button
        disabled={denied}
        title={denied ? missingPermission(check) : undefined}
        onClick={() => open(shellTab({ cluster, namespace, pod: name, container }, ++serial.current))}
      >
        Open shell
      </Button>
    </div>
  );
}
