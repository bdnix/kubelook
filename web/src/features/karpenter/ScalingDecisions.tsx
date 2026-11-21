import { useState } from 'react';
import { formatAge } from '../../lib/age';
import { getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import { classifyEvent, type Decision } from './classify';

const TABS: [Decision, string][] = [
  ['scaleUp', 'Scale up'],
  ['scaleDown', 'Scale down'],
  ['drift', 'Drift'],
  ['errors', 'Errors'],
  ['other', 'Other'],
];

const when = (e: K8sObject) =>
  getString(e, 'lastTimestamp') || getString(e, 'eventTime') || getString(e, 'metadata.creationTimestamp');
const objectOf = (e: K8sObject) => {
  const ref = (e.involvedObject ?? e.regarding) as { kind?: string; name?: string } | undefined;
  return ref?.kind ? `${ref.kind} ${ref.name ?? ''}` : '';
};

/** The Karpenter events, sorted into what they are about. Only the events the cluster still holds are listed. */
export function ScalingDecisions({ events, now }: { events: K8sObject[]; now: number }) {
  const [tab, setTab] = useState<Decision>('scaleUp');
  const groups = new Map<Decision, K8sObject[]>(TABS.map(([d]) => [d, []]));
  for (const e of events)
    groups.get(classifyEvent({ type: getString(e, 'type'), reason: getString(e, 'reason') }))?.push(e);
  const shown = [...(groups.get(tab) ?? [])].sort((a, b) => Date.parse(when(b)) - Date.parse(when(a)));
  const label = TABS.find(([d]) => d === tab)?.[1] ?? '';
  return (
    <>
      <p>
        Kubernetes keeps events for a short time only, so this lists only the events the cluster still holds.
      </p>
      <div role="tablist" aria-label="Decisions">
        {TABS.map(([d, l]) => (
          <button key={d} type="button" role="tab" aria-selected={tab === d} onClick={() => setTab(d)}>
            {`${l} (${groups.get(d)?.length ?? 0})`}
          </button>
        ))}
      </div>
      <ul aria-label={label}>
        {shown.map((e, i) => (
          <li key={i}>
            <strong>{getString(e, 'reason')}</strong> {getString(e, 'message') || getString(e, 'note')}{' '}
            <em>{objectOf(e)}</em> {formatAge(when(e), now)}
          </li>
        ))}
      </ul>
    </>
  );
}
