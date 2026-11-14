import { FitAddon } from '@xterm/addon-fit';
import { Terminal as XTerm } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';
import { useEffect, useRef } from 'react';

/** What the rest of the app may do with a terminal. */
export interface TerminalHandle {
  write(bytes: Uint8Array): void;
  onData(cb: (data: string) => void): void;
  onResize(cb: (cols: number, rows: number) => void): void;
  fit(): void;
  size(): { cols: number; rows: number };
}

/** A terminal. It only wires xterm to the handle; everything testable lives outside this folder. */
export function Terminal({ onReady, label }: { onReady: (handle: TerminalHandle) => void; label: string }) {
  const host = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const term = new XTerm({ cursorBlink: true, convertEol: false });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host.current as HTMLDivElement);
    fit.fit();
    const observer = new ResizeObserver(() => fit.fit());
    observer.observe(host.current as HTMLDivElement);
    onReady({
      write: (b) => term.write(b),
      onData: (cb) => void term.onData(cb),
      onResize: (cb) => void term.onResize((s) => cb(s.cols, s.rows)),
      fit: () => fit.fit(),
      size: () => ({ cols: term.cols, rows: term.rows }),
    });
    return () => {
      observer.disconnect();
      term.dispose();
    };
  }, [onReady]);
  return <div ref={host} role="group" aria-label={label} className="terminal" />;
}
