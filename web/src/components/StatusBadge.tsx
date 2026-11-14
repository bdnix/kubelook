import type { Severity, Status } from '../kinds/types';

const ICONS: Record<Severity, { icon: string; label: string }> = {
  ok: { icon: '✓', label: 'healthy' },
  warning: { icon: '!', label: 'needs attention' },
  error: { icon: '✕', label: 'failing' },
  unknown: { icon: '?', label: 'unknown' },
};

/** A status as text plus an icon with its own label: colour is never the only signal. */
export function StatusBadge({ status }: { status: Status }) {
  const { icon, label } = ICONS[status.severity];
  return (
    <span className={`badge badge-${status.severity}`}>
      <span role="img" aria-label={label}>
        {icon}
      </span>{' '}
      {status.text}
    </span>
  );
}
