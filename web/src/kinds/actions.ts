import type { AccessCheck } from '../api/access';

export interface ActionDef {
  id: string;
  label: string;
  /** Needs a confirmation that names the object. */
  destructive: boolean;
  /** The permission the action needs, for the menu to enable or disable it. */
  access: (t: ActionTarget) => AccessCheck;
}

export interface ActionTarget {
  group: string;
  resource: string;
  namespace: string;
  name: string;
}

const onObject =
  (verb: string) =>
  (t: ActionTarget): AccessCheck => ({
    verb,
    group: t.group,
    resource: t.resource,
    namespace: t.namespace || undefined,
    name: t.name,
  });

const patch = onObject('patch');

/** Every action of the UI. Later steps add rows. */
export const ACTIONS: Record<string, ActionDef> = {
  scale: {
    id: 'scale',
    label: 'Scale…',
    destructive: false,
    access: (t) => ({ ...patch(t), subresource: 'scale' }),
  },
  restart: { id: 'restart', label: 'Rollout restart', destructive: true, access: patch },
  pause: { id: 'pause', label: 'Pause rollout', destructive: false, access: patch },
  resume: { id: 'resume', label: 'Resume rollout', destructive: false, access: patch },
  rollback: { id: 'rollback', label: 'Roll back…', destructive: true, access: patch },
  setImage: { id: 'setImage', label: 'Set image…', destructive: false, access: patch },
  cronjobTrigger: {
    id: 'cronjobTrigger',
    label: 'Trigger now',
    destructive: false,
    access: (t) => ({
      verb: 'create',
      group: 'batch',
      resource: 'jobs',
      namespace: t.namespace || undefined,
    }),
  },
  cronjobSuspend: { id: 'cronjobSuspend', label: 'Suspend', destructive: false, access: patch },
  cronjobResume: { id: 'cronjobResume', label: 'Resume', destructive: false, access: patch },
  evict: {
    id: 'evict',
    label: 'Evict',
    destructive: true,
    access: (t) => ({
      verb: 'create',
      group: '',
      resource: 'pods',
      subresource: 'eviction',
      namespace: t.namespace || undefined,
      name: t.name,
    }),
  },
  expandPVC: { id: 'expandPVC', label: 'Expand…', destructive: false, access: patch },
  delete: { id: 'delete', label: 'Delete', destructive: true, access: onObject('delete') },
};

/** The permission text of a disabled action, such as `missing permission: patch deployments/scale`. */
export function missingPermission(c: AccessCheck): string {
  return `missing permission: ${c.verb} ${c.resource}${c.subresource ? `/${c.subresource}` : ''}`;
}
