import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { decodeUtf8, humanSize } from '../../lib/bytes';
import { browserSaveFile, type SaveFile } from '../../lib/saveFile';
import { getString } from '../../lib/path';
import type { K8sObject } from '../../state/live';
import type { Clipboard } from '../actions/ConfirmDialog';
import { RevealButton } from './RevealButton';
import { useReveal } from './useReveal';

const browserClipboard: Clipboard = { writeText: (t) => navigator.clipboard.writeText(t) };

/** The keys of a Secret. Values stay hidden until the user reveals them, one by one or all at once. */
export function SecretData({
  cluster,
  secret,
  saveFile = browserSaveFile,
  clipboard = browserClipboard,
}: {
  cluster: string;
  secret: K8sObject;
  saveFile?: SaveFile;
  clipboard?: Clipboard;
}) {
  const namespace = getString(secret, 'metadata.namespace');
  const name = getString(secret, 'metadata.name');
  const { values, reveal, hide, busy, error } = useReveal(cluster, namespace, name);
  const keys = Object.keys((secret.data as Record<string, unknown> | undefined) ?? {}).sort();
  if (keys.length === 0) return <p>This Secret has no keys.</p>;
  const allRevealed = keys.every((k) => values[k] !== undefined);

  return (
    <section aria-label="Secret data">
      <h3>Data</h3>
      <Button onClick={() => (allRevealed ? hide() : void reveal())} disabled={busy}>
        {allRevealed ? 'Hide all' : 'Reveal all'}
      </Button>
      <ErrorText message={error} />
      <ul>
        {keys.map((key) => {
          const bytes = values[key];
          const text = bytes ? decodeUtf8(bytes) : null;
          return (
            <li key={key}>
              <strong>{key}</strong>: {bytes === undefined && <span>••••••••</span>}
              {bytes !== undefined && text !== null && (
                <>
                  <code>{text}</code>{' '}
                  <Button onClick={() => void clipboard.writeText(text)}>{`Copy ${key}`}</Button>
                </>
              )}
              {bytes !== undefined && text === null && (
                <>
                  <span>{`binary, ${humanSize(bytes.length)} (${bytes.length} bytes)`}</span>{' '}
                  <Button onClick={() => saveFile(key, bytes)}>{`Download ${key}`}</Button>
                </>
              )}{' '}
              <RevealButton
                name={key}
                revealed={bytes !== undefined}
                busy={busy}
                onReveal={() => void reveal([key])}
                onHide={() => hide(key)}
              />
            </li>
          );
        })}
      </ul>
    </section>
  );
}
