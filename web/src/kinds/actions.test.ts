import { describe, expect, it } from 'vitest';
import { ACTIONS, missingPermission, type ActionTarget } from './actions';

const dep: ActionTarget = { group: 'apps', resource: 'deployments', namespace: 'ns', name: 'web' };

describe('action permissions', () => {
  it.each([
    [
      'scale',
      {
        verb: 'patch',
        group: 'apps',
        resource: 'deployments',
        subresource: 'scale',
        namespace: 'ns',
        name: 'web',
      },
    ],
    ['restart', { verb: 'patch', group: 'apps', resource: 'deployments', namespace: 'ns', name: 'web' }],
    ['delete', { verb: 'delete', group: 'apps', resource: 'deployments', namespace: 'ns', name: 'web' }],
    ['cronjobTrigger', { verb: 'create', group: 'batch', resource: 'jobs', namespace: 'ns' }],
    [
      'evict',
      { verb: 'create', group: '', resource: 'pods', subresource: 'eviction', namespace: 'ns', name: 'web' },
    ],
  ])('%s needs %j', (id, want) => {
    expect(ACTIONS[id]?.access(dep)).toEqual(want);
  });

  it('cluster-scoped targets have no namespace', () => {
    expect(
      ACTIONS.delete?.access({ group: '', resource: 'nodes', namespace: '', name: 'n1' })?.namespace,
    ).toBeUndefined();
  });

  it('destructive actions are delete, evict, rollback and restart', () => {
    expect(
      Object.values(ACTIONS)
        .filter((a) => a.destructive)
        .map((a) => a.id)
        .sort(),
    ).toEqual(['delete', 'evict', 'restart', 'rollback']);
  });

  it('names the missing permission', () => {
    expect(
      missingPermission({ verb: 'patch', group: 'apps', resource: 'deployments', subresource: 'scale' }),
    ).toBe('missing permission: patch deployments/scale');
    expect(missingPermission({ verb: 'delete', group: '', resource: 'pods' })).toBe(
      'missing permission: delete pods',
    );
  });
});
