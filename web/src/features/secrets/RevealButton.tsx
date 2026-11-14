import { Button } from '../../components/Button';

/** Shows or hides one value. */
export function RevealButton({
  name,
  revealed,
  busy,
  onReveal,
  onHide,
}: {
  name: string;
  revealed: boolean;
  busy: boolean;
  onReveal: () => void;
  onHide: () => void;
}) {
  return revealed ? (
    <Button onClick={onHide} aria-label={`Hide ${name}`}>
      Hide
    </Button>
  ) : (
    <Button onClick={onReveal} disabled={busy} aria-label={`Reveal ${name}`}>
      Reveal
    </Button>
  );
}
