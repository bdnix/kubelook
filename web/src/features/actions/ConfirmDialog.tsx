import { useEffect, useRef, type ReactNode } from 'react';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';

export interface Clipboard {
  writeText(text: string): Promise<void>;
}

/** The browser clipboard; tests pass their own. */
const browserClipboard: Clipboard = { writeText: (t) => navigator.clipboard.writeText(t) };

export interface ConfirmDialogProps {
  title: string;
  confirmLabel: string;
  destructive?: boolean;
  busy?: boolean;
  error?: string;
  /** Disables the confirm button while the input is not valid. */
  canConfirm?: boolean;
  /** The kubectl command that does the same; offered with a Copy button. */
  kubectl: string;
  onConfirm: () => void;
  onCancel: () => void;
  clipboard?: Clipboard;
  children?: ReactNode;
}

/** A modal that says what will happen, offers the kubectl equivalent and asks to confirm. */
export function ConfirmDialog({
  title,
  confirmLabel,
  destructive = false,
  busy = false,
  error,
  canConfirm = true,
  kubectl,
  onConfirm,
  onCancel,
  clipboard = browserClipboard,
  children,
}: ConfirmDialogProps) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => ref.current?.focus(), []);
  return (
    <div
      ref={ref}
      role="dialog"
      aria-modal="true"
      aria-label={title}
      tabIndex={-1}
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.stopPropagation();
          onCancel();
        }
      }}
    >
      <h2>{title}</h2>
      {children}
      <p>
        <code>{kubectl}</code>{' '}
        <Button onClick={() => void clipboard.writeText(kubectl)}>Copy as kubectl</Button>
      </p>
      <ErrorText message={error} />
      <Button onClick={onCancel}>Cancel</Button>
      <Button
        onClick={onConfirm}
        disabled={busy || !canConfirm}
        className={destructive ? 'danger' : undefined}
      >
        {confirmLabel}
      </Button>
    </div>
  );
}
