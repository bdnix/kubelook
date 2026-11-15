import { describe, expect, it } from 'vitest';
import type { Column } from '../../kinds/types';
import { applyPreset, movePreset, presetKey, togglePreset } from './columnPreset';

const col = (id: string): Column => ({ id, title: id.toUpperCase(), value: () => id });
const columns = ['name', 'status', 'age', 'node'].map(col);
const ids = (cs: Column[]) => cs.map((c) => c.id);

describe('presetKey', () => {
  it('is group/kind, with the core group empty', () => {
    expect(presetKey('', 'Pod')).toBe('/Pod');
    expect(presetKey('core', 'Pod')).toBe('/Pod');
    expect(presetKey('apps', 'Deployment')).toBe('apps/Deployment');
  });
});

describe('applyPreset', () => {
  it('shows the columns of the preset, in its order', () => {
    expect(ids(applyPreset(columns, ['age', 'name']))).toEqual(['age', 'name']);
  });
  it('ignores ids the kind does not have, and shows everything without a usable preset', () => {
    expect(ids(applyPreset(columns, ['gone', 'status']))).toEqual(['status']);
    expect(ids(applyPreset(columns, undefined))).toEqual(['name', 'status', 'age', 'node']);
    expect(ids(applyPreset(columns, []))).toEqual(['name', 'status', 'age', 'node']);
    expect(ids(applyPreset(columns, ['gone']))).toEqual(['name', 'status', 'age', 'node']);
  });
});

describe('togglePreset and movePreset', () => {
  const all = ['name', 'status', 'age', 'node'];
  it('hides a visible column and shows a hidden one at the end', () => {
    expect(togglePreset(all, undefined, 'age')).toEqual(['name', 'status', 'node']);
    expect(togglePreset(all, ['name', 'status'], 'node')).toEqual(['name', 'status', 'node']);
    expect(togglePreset(all, ['name', 'status'], 'status')).toEqual(['name']);
  });
  it('the last visible column stays', () => {
    expect(togglePreset(all, ['name'], 'name')).toEqual(['name']);
  });
  it('moves a visible column up or down', () => {
    expect(movePreset(all, undefined, 'status', -1)).toEqual(['status', 'name', 'age', 'node']);
    expect(movePreset(all, ['name', 'age'], 'name', 1)).toEqual(['age', 'name']);
    expect(movePreset(all, ['name', 'age'], 'name', -1)).toEqual(['name', 'age']);
    expect(movePreset(all, ['name', 'age'], 'age', 1)).toEqual(['name', 'age']);
  });
});
