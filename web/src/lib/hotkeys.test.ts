import { describe, expect, it } from 'vitest';
import { createHotkeys, isTypingTarget } from './hotkeys';

const key = (
  k: string,
  extra: Partial<{ ctrl: boolean; meta: boolean; alt: boolean; typing: boolean; at: number }> = {},
) => ({
  key: k,
  ctrl: false,
  meta: false,
  alt: false,
  typing: false,
  at: 0,
  ...extra,
});

describe('createHotkeys', () => {
  it('g then p goes to Pods', () => {
    const h = createHotkeys();
    expect(h.press(key('g', { at: 0 }))).toBeUndefined();
    expect(h.press(key('p', { at: 400 }))).toEqual({ type: 'go', kind: 'Pod' });
  });

  it('the second key must follow within a moment', () => {
    const h = createHotkeys();
    h.press(key('g', { at: 0 }));
    expect(h.press(key('p', { at: 5000 }))).toBeUndefined();
  });

  it('p alone does nothing, and another key after g forgets the g', () => {
    const h = createHotkeys();
    expect(h.press(key('p'))).toBeUndefined();
    h.press(key('g'));
    expect(h.press(key('x'))).toBeUndefined();
    expect(h.press(key('p'))).toBeUndefined();
  });

  it('/ focuses the list filter and ? opens the help', () => {
    const h = createHotkeys();
    expect(h.press(key('/'))).toEqual({ type: 'focusFilter' });
    expect(h.press(key('?'))).toEqual({ type: 'help' });
  });

  it('ignores keys typed into inputs, the editor or a terminal, and keys with modifiers', () => {
    const h = createHotkeys();
    expect(h.press(key('/', { typing: true }))).toBeUndefined();
    expect(h.press(key('?', { typing: true }))).toBeUndefined();
    h.press(key('g', { typing: true }));
    expect(h.press(key('p'))).toBeUndefined();
    expect(h.press(key('/', { ctrl: true }))).toBeUndefined();
    expect(h.press(key('?', { meta: true }))).toBeUndefined();
    h.press(key('g'));
    expect(h.press(key('p', { alt: true }))).toBeUndefined();
  });
});

describe('isTypingTarget', () => {
  const el = (html: string) => {
    const d = document.createElement('div');
    d.innerHTML = html;
    return d.firstElementChild as HTMLElement;
  };
  it.each(['<input>', '<textarea></textarea>', '<select></select>', '<div contenteditable="true"></div>'])(
    '%s is typing',
    (html) => {
      expect(isTypingTarget(el(html))).toBe(true);
    },
  );
  it('anything inside the editor or a terminal is typing', () => {
    const root = el('<div class="monaco-editor"><div class="inner"><span id="x"></span></div></div>');
    expect(isTypingTarget(root.querySelector('#x'))).toBe(true);
    expect(
      isTypingTarget(el('<div class="xterm"><textarea></textarea></div>').querySelector('textarea')),
    ).toBe(true);
  });
  it('buttons, links and the page are not', () => {
    expect(isTypingTarget(el('<button></button>'))).toBe(false);
    expect(isTypingTarget(el('<a href="#">x</a>'))).toBe(false);
    expect(isTypingTarget(null)).toBe(false);
  });
});
