import { useState } from 'react';
import { useAccess, type AccessCheck } from '../../api/access';
import { Button } from '../../components/Button';
import { ACTIONS, missingPermission } from '../../kinds/actions';
import { getArray, getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { kubectlCommand } from '../editor/kubectlCommand';
import { ConfirmDialog, type Clipboard } from './ConfirmDialog';
import { ExpandDialog } from './ExpandDialog';
import { RollbackDialog, type Revision } from './RollbackDialog';
import { ScaleDialog } from './ScaleDialog';
import { SetImageDialog, type ContainerChoice } from './SetImageDialog';
import { useAction } from './useAction';

export interface ActionMenuProps {
  cluster: string;
  object: K8sObject;
  group: string;
  version: string;
  resource: string;
  kind: string;
  /** The action ids the kind offers (`KindDescriptor.actions`). Unknown ids are ignored. */
  actions: string[];
  /** Everything is disabled, for an object that is gone. */
  disabled?: boolean;
  /** The Deployment's earlier revisions for the rollback dialog. */
  revisions?: Revision[];
  clipboard?: Clipboard;
}

function containerChoices(o: K8sObject): ContainerChoice[] {
  const pick = (path: string, init: boolean) =>
    getArray(o, path).map((c) => ({ name: getString(c, 'name'), image: getString(c, 'image'), init }));
  return [
    ...pick('spec.template.spec.containers', false),
    ...pick('spec.template.spec.initContainers', true),
  ];
}

/** The "Actions" menu of an object: each action is enabled by the user's own RBAC, and dialogs collect parameters. */
export function ActionMenu({
  cluster,
  object,
  group,
  version,
  resource,
  kind,
  actions,
  disabled = false,
  revisions = [],
  clipboard,
}: ActionMenuProps) {
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState<string>();
  const { run, busy, error, reset } = useAction(cluster);
  const name = getString(object, 'metadata.name');
  const namespace = getString(object, 'metadata.namespace');
  const known = actions.filter((a) => ACTIONS[a] !== undefined);
  const target = { group, resource, namespace, name };
  const checks: AccessCheck[] = known.map((a) => (ACTIONS[a] as (typeof ACTIONS)[string]).access(target));
  const access = useAccess(cluster, checks, known.length > 0);
  if (known.length === 0) return null;

  const ref = { group, version, resource, namespace, name };
  const close = () => {
    setActive(undefined);
    reset();
  };
  const exec = async (action: string, params?: unknown) => {
    if (await run(action, ref, params)) close();
  };
  const kubectl = (a: 'delete' | 'evict' | 'restart'): string => {
    const r = resource.replace(/s$/, '');
    if (a === 'evict') return kubectlCommand({ type: 'evict', name, namespace });
    return kubectlCommand(
      a === 'delete'
        ? { type: 'delete', resource: r, name, namespace }
        : { type: 'restart', resource: r, name, namespace },
    );
  };

  const choose = (id: string) => {
    setOpen(false);
    const def = ACTIONS[id];
    const direct = !def?.destructive && !['scale', 'setImage', 'expandPVC', 'rollback'].includes(id);
    if (direct) void exec(id);
    else setActive(id);
  };

  return (
    <div className="action-menu">
      <Button aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)} disabled={disabled}>
        Actions
      </Button>
      {open && (
        <div role="menu" aria-label="Actions">
          {known.map((id, i) => {
            const allowed = access.data?.[i]?.allowed;
            const denied = allowed === false;
            return (
              <button
                key={id}
                type="button"
                role="menuitem"
                disabled={denied}
                title={denied ? missingPermission(checks[i] as AccessCheck) : undefined}
                onClick={() => choose(id)}
              >
                {(ACTIONS[id] as (typeof ACTIONS)[string]).label}
              </button>
            );
          })}
        </div>
      )}
      {active === 'scale' && (
        <ScaleDialog
          kind={kind}
          resource={resource}
          name={name}
          namespace={namespace}
          current={Number((object.spec as { replicas?: number } | undefined)?.replicas ?? 1)}
          busy={busy}
          error={error}
          onConfirm={(replicas) => void exec('scale', { replicas })}
          onCancel={close}
          clipboard={clipboard}
        />
      )}
      {active === 'setImage' && (
        <SetImageDialog
          kind={kind}
          resource={resource}
          name={name}
          namespace={namespace}
          containers={containerChoices(object)}
          busy={busy}
          error={error}
          onConfirm={(p) => void exec('setImage', p)}
          onCancel={close}
          clipboard={clipboard}
        />
      )}
      {active === 'rollback' && (
        <RollbackDialog
          name={name}
          namespace={namespace}
          revisions={revisions}
          busy={busy}
          error={error}
          onConfirm={(revision) => void exec('rollback', { revision })}
          onCancel={close}
          clipboard={clipboard}
        />
      )}
      {active === 'expandPVC' && (
        <ExpandDialog
          name={name}
          namespace={namespace}
          current={getString(object, 'spec.resources.requests.storage')}
          busy={busy}
          error={error}
          onConfirm={(storage) => void exec('expandPVC', { storage })}
          onCancel={close}
          clipboard={clipboard}
        />
      )}
      {(active === 'delete' || active === 'evict' || active === 'restart') && (
        <ConfirmDialog
          title={`${(ACTIONS[active] as (typeof ACTIONS)[string]).label.replace('Rollout restart', 'Restart')} ${kind} ${name}?`}
          confirmLabel={active === 'delete' ? 'Delete' : active === 'evict' ? 'Evict' : 'Restart'}
          destructive
          busy={busy}
          error={error}
          kubectl={kubectl(active)}
          onConfirm={() => void exec(active)}
          onCancel={close}
          clipboard={clipboard}
        >
          <p>{`This acts on ${kind} ${name}${namespace ? ` in ${namespace}` : ''}.`}</p>
        </ConfirmDialog>
      )}
    </div>
  );
}
