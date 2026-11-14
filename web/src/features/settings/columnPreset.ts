import type { Column } from '../../kinds/types';

/** The key of a kind in `columnPresets`: `<group>/<kind>`, the core group being empty. */
export function presetKey(group: string, kind: string): string {
  return `${group === 'core' ? '' : group}/${kind}`;
}

/** The columns of a preset, in its order. Unknown ids are ignored; without a usable preset all columns show. */
export function applyPreset(columns: Column[], ids: string[] | undefined): Column[] {
  if (!ids || ids.length === 0) return columns;
  const byId = new Map(columns.map((c) => [c.id, c]));
  const picked = ids.flatMap((id) => byId.get(id) ?? []);
  return picked.length === 0 ? columns : picked;
}

/** Shows a hidden column (at the end) or hides a visible one; the last visible column stays. */
export function togglePreset(all: string[], current: string[] | undefined, id: string): string[] {
  const visible = current && current.length > 0 ? current : all;
  if (!visible.includes(id)) return [...visible, id];
  return visible.length === 1 ? visible : visible.filter((v) => v !== id);
}

/** Moves a visible column one place; at either end it stays. */
export function movePreset(
  all: string[],
  current: string[] | undefined,
  id: string,
  delta: -1 | 1,
): string[] {
  const visible = [...(current && current.length > 0 ? current : all)];
  const i = visible.indexOf(id);
  const j = i + delta;
  if (i < 0 || j < 0 || j >= visible.length) return visible;
  [visible[i], visible[j]] = [visible[j] as string, visible[i] as string];
  return visible;
}
