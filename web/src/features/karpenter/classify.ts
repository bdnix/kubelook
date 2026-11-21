export type Decision = 'drift' | 'errors' | 'scaleDown' | 'scaleUp' | 'other';

export interface EventLike {
  type?: string;
  reason?: string;
}

const has = (text: string, words: string[]) => words.some((w) => text.includes(w));

/**
 * What a Karpenter event is about, from its reason and type. This is a heuristic (assumption A29): the events
 * carry no machine-readable category, so a substring of the reason decides, drift first.
 */
export function classifyEvent(e: EventLike): Decision {
  const reason = (e.reason ?? '').toLowerCase();
  if (reason.includes('drift')) return 'drift';
  if ((e.type ?? '') === 'Warning' || has(reason, ['fail', 'error', 'insufficient'])) return 'errors';
  if (has(reason, ['disrupt', 'terminat', 'consolidat', 'evict', 'empty', 'expir'])) return 'scaleDown';
  if (has(reason, ['nominat', 'launch', 'register', 'initializ', 'provision'])) return 'scaleUp';
  return 'other';
}
