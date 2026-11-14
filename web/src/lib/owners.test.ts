import { describe, expect, it } from 'vitest';
import { groupOf, MAX_OWNER_LEVELS, ownerChain, ownerRefs, type OwnerRef } from './owners';

const owned = (name: string, ...owners: Array<[string, string, string, boolean?]>) => ({
  metadata: {
    name,
    ownerReferences: owners.map(([apiVersion, kind, ownerName, controller]) => ({
      apiVersion,
      kind,
      name: ownerName,
      uid: `${kind}/${ownerName}`,
      ...(controller === undefined ? {} : { controller }),
    })),
  },
});

const objects = [owned('web-abc', ['apps/v1', 'Deployment', 'web', true]), owned('web')];
const lookup = (ref: OwnerRef) =>
  Promise.resolve(
    ref.kind === 'ReplicaSet' ? objects[0] : ref.kind === 'Deployment' ? objects[1] : undefined,
  );

describe('owners', () => {
  it('ownerRefs reads the references', () => {
    expect(ownerRefs(owned('p', ['v1', 'Node', 'n1']))).toEqual([
      { apiVersion: 'v1', kind: 'Node', name: 'n1', uid: 'Node/n1', controller: false },
    ]);
    expect(ownerRefs({ metadata: {} })).toEqual([]);
  });

  it('groupOf splits the group from the version', () => {
    expect(groupOf('apps/v1')).toBe('apps');
    expect(groupOf('v1')).toBe('');
  });

  it('follows Pod → ReplicaSet → Deployment', async () => {
    const pod = owned('web-abc-1', ['apps/v1', 'ReplicaSet', 'web-abc', true]);
    const chain = await ownerChain(pod, lookup);
    expect(chain.map((l) => `${l.ref.kind}/${l.ref.name}`)).toEqual(['ReplicaSet/web-abc', 'Deployment/web']);
    expect(chain[1]?.object).toBe(objects[1]);
  });

  it('prefers the controller reference', async () => {
    const pod = owned('p', ['v1', 'Node', 'n1', false], ['apps/v1', 'ReplicaSet', 'web-abc', true]);
    expect((await ownerChain(pod, lookup))[0]?.ref.kind).toBe('ReplicaSet');
  });

  it('ends with the unresolved owner when it is missing', async () => {
    const pod = owned('p', ['apps/v1', 'StatefulSet', 'gone', true]);
    const chain = await ownerChain(pod, () => Promise.resolve(undefined));
    expect(chain).toEqual([
      { ref: expect.objectContaining({ kind: 'StatefulSet', name: 'gone' }) as OwnerRef },
    ]);
  });

  it('is empty without owners and stops at a cycle', async () => {
    expect(await ownerChain({ metadata: {} }, lookup)).toEqual([]);
    const a = owned('a', ['v1', 'Loop', 'b', true]);
    const b = owned('b', ['v1', 'Loop', 'b', true]);
    expect(await ownerChain(a, () => Promise.resolve(b))).toHaveLength(1);
  });

  it('stops after five levels', async () => {
    const chainOf = (n: number): ReturnType<typeof owned> =>
      owned(`o${n}`, ['v1', 'Level', `o${n + 1}`, true]);
    const chain = await ownerChain(chainOf(0), (ref) => Promise.resolve(chainOf(Number(ref.name.slice(1)))));
    expect(chain).toHaveLength(MAX_OWNER_LEVELS);
  });
});
