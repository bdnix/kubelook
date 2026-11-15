export interface UsageBarProps {
  label: string;
  used: number;
  /** What it is measured against (a node's allocatable); 0 when unknown. */
  total: number;
  format: (n: number) => string;
}

/** A meter with the numbers beside it as text. */
export function UsageBar({ label, used, total, format }: UsageBarProps) {
  const percent = total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0;
  return (
    <div className="usage-bar">
      <span>{label}</span>{' '}
      <div
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        className="meter"
      >
        <div className="meter-fill" style={{ width: `${percent}%` }} />
      </div>{' '}
      <span>
        {total > 0
          ? `${format(used)} of ${format(total)} (${percent}%)`
          : `${format(used)} (allocatable unknown)`}
      </span>
    </div>
  );
}
