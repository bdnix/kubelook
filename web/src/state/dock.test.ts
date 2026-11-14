import { describe, expect, it } from 'vitest';
import { createDockStore, logTab } from './dock';

const tab = (pod: string) => logTab({ cluster: 'v:prod', namespace: 'ns', pod, containers: ['app'] });

describe('dock store', () => {
  it('opens tabs, activates the newest and does not duplicate', () => {
    const s = createDockStore();
    s.getState().open(tab('a'));
    s.getState().open(tab('b'));
    s.getState().open(tab('a'));
    expect(s.getState().tabs.map((t) => t.title)).toEqual(['Logs: a', 'Logs: b']);
    expect(s.getState().active).toBe(tab('a').id);
  });

  it('closing the active tab activates its neighbour; closing the last leaves none', () => {
    const s = createDockStore();
    for (const p of ['a', 'b', 'c']) s.getState().open(tab(p));
    s.getState().activate(tab('b').id);
    s.getState().close(tab('b').id);
    expect(s.getState().active).toBe(tab('c').id);
    s.getState().close(tab('a').id); // not active: the active tab stays
    expect(s.getState().active).toBe(tab('c').id);
    s.getState().close(tab('c').id);
    expect(s.getState().tabs).toEqual([]);
    expect(s.getState().active).toBeNull();
  });

  it('ignores unknown tabs', () => {
    const s = createDockStore();
    s.getState().open(tab('a'));
    s.getState().close('nope');
    s.getState().activate('nope');
    expect(s.getState().tabs).toHaveLength(1);
    expect(s.getState().active).toBe(tab('a').id);
  });
});
