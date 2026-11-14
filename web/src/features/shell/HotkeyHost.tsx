import { useEffect, useRef, useState } from 'react';
import { useMatch, useNavigate } from 'react-router';
import { createHotkeys, isTypingTarget } from '../../lib/hotkeys';
import { clusterPath } from '../../lib/paths';
import { useFocusTrap } from '../../lib/useFocusTrap';
import { Button } from '../../components/Button';

/** The input that filters the list in view: the one labelled "Field filter". */
function findListFilter(): HTMLInputElement | undefined {
  return [...document.querySelectorAll('input')].find(
    (i) =>
      i.getAttribute('aria-label') === 'Field filter' ||
      i.labels?.[0]?.textContent?.startsWith('Field filter'),
  );
}

const SHORTCUTS: Array<[string, string]> = [
  ['g then p', 'Go to the pods of the open cluster'],
  ['/', 'Filter the list'],
  ['Ctrl+K or Cmd+K', 'Open the command palette'],
  ['?', 'Show this help'],
];

function Help({ onClose }: { onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useFocusTrap(ref);
  useEffect(() => ref.current?.focus(), []);
  return (
    <div
      ref={ref}
      role="dialog"
      aria-modal="true"
      aria-label="Keyboard shortcuts"
      tabIndex={-1}
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.stopPropagation();
          onClose();
        }
      }}
    >
      <h2>Keyboard shortcuts</h2>
      <dl>
        {SHORTCUTS.map(([keys, what]) => (
          <div key={keys}>
            <dt>
              <kbd>{keys}</kbd>
            </dt>
            <dd>{what}</dd>
          </div>
        ))}
      </dl>
      <Button onClick={onClose}>Close</Button>
    </div>
  );
}

/** Listens for the shortcuts of the whole app and shows the help. Renders nothing until the help is asked for. */
export function HotkeyHost() {
  const navigate = useNavigate();
  const match = useMatch('/c/:cluster/*');
  const cluster = match?.params['cluster'];
  const [help, setHelp] = useState(false);
  const hotkeys = useRef(createHotkeys());

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const action = hotkeys.current.press({
        key: e.key,
        ctrl: e.ctrlKey,
        meta: e.metaKey,
        alt: e.altKey,
        typing: isTypingTarget(e.target instanceof Element ? e.target : null),
        at: e.timeStamp,
      });
      if (!action) return;
      if (action.type === 'go') {
        if (cluster === undefined) return;
        e.preventDefault();
        void navigate(`${clusterPath(cluster)}/core/${action.kind}`);
      } else if (action.type === 'focusFilter') {
        const input = findListFilter();
        if (!input) return;
        e.preventDefault();
        input.focus();
      } else {
        e.preventDefault();
        setHelp(true);
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [cluster, navigate]);

  return help ? <Help onClose={() => setHelp(false)} /> : null;
}
