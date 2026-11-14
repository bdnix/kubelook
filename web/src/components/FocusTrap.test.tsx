import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { ConfirmDialog } from '../features/actions/ConfirmDialog';
import { SmallScreenBanner } from './SmallScreenBanner';

function Host() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button onClick={() => setOpen(true)}>Open</button>
      {open && (
        <ConfirmDialog
          title="Sure?"
          confirmLabel="Yes"
          kubectl="kubectl x"
          onConfirm={vi.fn()}
          onCancel={() => setOpen(false)}
        />
      )}
    </>
  );
}

describe('dialogs', () => {
  it('keep Tab inside, in both directions, and give the focus back when they close', async () => {
    render(<Host />);
    const opener = screen.getByRole('button', { name: 'Open' });
    await userEvent.click(opener);
    const dialog = screen.getByRole('dialog');
    const inside = [
      screen.getByRole('button', { name: 'Copy as kubectl' }),
      screen.getByRole('button', { name: 'Cancel' }),
      screen.getByRole('button', { name: 'Yes' }),
    ];
    expect(dialog).toHaveFocus();
    await userEvent.tab();
    expect(inside[0]).toHaveFocus();
    await userEvent.tab();
    await userEvent.tab();
    expect(inside[2]).toHaveFocus();
    await userEvent.tab();
    expect(inside[0]).toHaveFocus(); // wrapped around, not out
    await userEvent.tab({ shift: true });
    expect(inside[2]).toHaveFocus();
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(opener).toHaveFocus();
  });
});

describe('SmallScreenBanner', () => {
  const media = (matches: boolean) => (query: string) =>
    ({
      matches,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }) as unknown as MediaQueryList;

  it('asks for a desktop browser below 1024 px', () => {
    render(<SmallScreenBanner matchMedia={media(false)} />);
    expect(screen.getByRole('status')).toHaveTextContent('Kubelook is designed for desktop browsers.');
  });

  it('shows nothing on a wide screen', () => {
    render(<SmallScreenBanner matchMedia={media(true)} />);
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('follows the screen width', () => {
    let listener: (() => void) | undefined;
    const mql = {
      matches: true,
      addEventListener: (_: string, fn: () => void) => (listener = fn),
      removeEventListener: vi.fn(),
    };
    render(<SmallScreenBanner matchMedia={() => mql as unknown as MediaQueryList} />);
    expect(screen.queryByRole('status')).toBeNull();
    mql.matches = false;
    act(() => listener?.());
    expect(screen.getByRole('status')).toHaveTextContent('Kubelook is designed for desktop browsers.');
  });
});
