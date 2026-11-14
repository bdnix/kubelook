/** One key press, as the host sees it. `typing` is set when the key goes to an input, the editor or a terminal. */
export interface KeyPress {
  key: string;
  ctrl: boolean;
  meta: boolean;
  alt: boolean;
  typing: boolean;
  /** When it happened, in milliseconds. */
  at: number;
}

export type HotkeyAction = { type: 'go'; kind: 'Pod' } | { type: 'focusFilter' } | { type: 'help' };

/** How long after `g` the next key still counts. */
export const SEQUENCE_MS = 1500;

/** The shortcuts: `g` then `p`, `/` and `?`. Keys with a modifier, and keys typed into fields, are never shortcuts. */
export function createHotkeys() {
  let armedAt: number | undefined;
  return {
    press(k: KeyPress): HotkeyAction | undefined {
      if (k.typing) {
        armedAt = undefined;
        return undefined;
      }
      if (k.ctrl || k.meta || k.alt) {
        armedAt = undefined;
        return undefined;
      }
      const armed = armedAt !== undefined && k.at - armedAt <= SEQUENCE_MS;
      armedAt = undefined;
      if (k.key === 'g') {
        armedAt = k.at;
        return undefined;
      }
      if (k.key === 'p' && armed) return { type: 'go', kind: 'Pod' };
      if (k.key === '/') return { type: 'focusFilter' };
      if (k.key === '?') return { type: 'help' };
      return undefined;
    },
  };
}

const TYPING =
  'input, textarea, select, [contenteditable=""], [contenteditable="true"], .monaco-editor, .xterm';

/** True for the places where keys are text: fields, the code editor and terminals. */
export function isTypingTarget(el: Element | null): boolean {
  return el !== null && el.closest(TYPING) !== null;
}
