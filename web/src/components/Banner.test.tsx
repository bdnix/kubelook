import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { Banner } from './Banner';

describe('Banner', () => {
  it('info uses role status', () => {
    render(<Banner kind="info">Hello</Banner>);
    expect(screen.getByRole('status')).toHaveTextContent('Hello');
  });

  it.each(['warning', 'error'] as const)('%s uses role alert', (kind) => {
    render(<Banner kind={kind}>Careful</Banner>);
    expect(screen.getByRole('alert')).toHaveTextContent('Careful');
  });

  it('shows an action button that calls its handler', async () => {
    const onClick = vi.fn();
    render(
      <Banner kind="warning" action={{ label: 'Retry', onClick }}>
        Lost connection
      </Banner>,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('has no button without an action', () => {
    render(<Banner kind="info">Hello</Banner>);
    expect(screen.queryByRole('button')).toBeNull();
  });
});
