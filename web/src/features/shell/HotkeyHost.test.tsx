import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { describe, expect, it } from 'vitest';
import { HotkeyHost } from './HotkeyHost';

function Where() {
  return <p data-testid="where">{decodeURIComponent(useLocation().pathname)}</p>;
}

function mount(route = '/c/v%3Aprod/core/Node') {
  render(
    <MemoryRouter initialEntries={[route]}>
      <HotkeyHost />
      <input aria-label="Field filter" />
      <input aria-label="Other" />
      <Routes>
        <Route path="*" element={<Where />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('HotkeyHost', () => {
  it('g then p opens the pods of the open cluster', async () => {
    mount();
    await userEvent.keyboard('gp');
    expect(screen.getByTestId('where')).toHaveTextContent('/c/v:prod/core/Pod');
  });

  it('g then p does nothing without an open cluster', async () => {
    mount('/settings');
    await userEvent.keyboard('gp');
    expect(screen.getByTestId('where')).toHaveTextContent('/settings');
  });

  it('/ focuses the list filter', async () => {
    mount();
    await userEvent.keyboard('/');
    expect(screen.getByLabelText('Field filter')).toHaveFocus();
    expect(screen.getByLabelText('Field filter')).toHaveValue('');
  });

  it('? opens the shortcut help, which lists the shortcuts and closes with Escape', async () => {
    mount();
    await userEvent.keyboard('?');
    const help = screen.getByRole('dialog', { name: 'Keyboard shortcuts' });
    expect(help).toHaveTextContent('g then p');
    expect(help).toHaveTextContent('Ctrl+K');
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('ignores the keys while typing in a field', async () => {
    mount();
    await userEvent.click(screen.getByLabelText('Other'));
    await userEvent.keyboard('gp/?');
    expect(screen.getByLabelText('Other')).toHaveValue('gp/?');
    expect(screen.getByTestId('where')).toHaveTextContent('/c/v:prod/core/Node');
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
