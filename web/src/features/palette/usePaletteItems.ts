import { useMatch } from 'react-router';
import { findResource, useDiscovery } from '../../api/discovery';
import { knownKinds } from '../../kinds/registry';
import { clusterPath } from '../../lib/paths';

export interface PaletteItem {
  id: string;
  title: string;
  /** Where choosing it goes. */
  path: string;
}

/**
 * How well `query` matches `title`: -1 when its letters do not appear in order, otherwise a score that is higher
 * for matches that start early, run together and begin words.
 */
export function fuzzyScore(query: string, title: string): number {
  if (query === '') return 0;
  const q = query.toLowerCase();
  const t = title.toLowerCase();
  let at = 0;
  let score = 100 - t.length * 0.5;
  let previous = -2;
  for (const ch of q) {
    const i = t.indexOf(ch, at);
    if (i < 0) return -1;
    score += previous === i - 1 ? 8 : 0;
    score += i === 0 || t[i - 1] === ' ' ? 6 : 0;
    score -= (i - at) * 0.5;
    previous = i;
    at = i + 1;
  }
  return score;
}

/** What the palette offers: the views and kinds of the open cluster (never of another one), and the settings. */
export function usePaletteItems(): PaletteItem[] {
  const match = useMatch('/c/:cluster/*');
  const cluster = match?.params['cluster'];
  const discovery = useDiscovery(cluster ?? '', cluster !== undefined);
  const items: PaletteItem[] = [];
  if (cluster !== undefined) {
    const base = clusterPath(cluster);
    for (const k of knownKinds()) {
      if (discovery.data && !findResource(discovery.data, k.group, k.kind)) continue;
      items.push({
        id: `kind:${k.group}/${k.kind}`,
        title: k.title,
        path: `${base}/${k.group || 'core'}/${k.kind}`,
      });
    }
    items.push({ id: 'view:pod-files', title: 'Pod Files', path: `${base}/pod-files` });
    items.push({ id: 'view:who-can', title: 'Who can…', path: `${base}/who-can` });
  }
  items.push({ id: 'view:settings', title: 'Settings', path: '/settings' });
  return items;
}
