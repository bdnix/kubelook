import type { ReactNode } from 'react';

export type BannerKind = 'info' | 'warning' | 'error';

export function Banner({
  kind,
  action,
  children,
}: {
  kind: BannerKind;
  action?: { label: string; onClick: () => void };
  children: ReactNode;
}) {
  return (
    <div className={`banner banner-${kind}`} role={kind === 'info' ? 'status' : 'alert'}>
      <span>{children}</span>
      {action && (
        <button type="button" onClick={action.onClick}>
          {action.label}
        </button>
      )}
    </div>
  );
}
