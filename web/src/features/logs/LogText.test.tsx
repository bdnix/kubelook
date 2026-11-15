import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { LogText } from './LogText';

describe('LogText', () => {
  it('renders URLs as safe links and the other tokens as classed text', () => {
    const { container } = render(
      <LogText text="2026-11-14T12:00:00Z [main] see https://example.com/x id 123e4567-e89b-12d3-a456-426614174000" />,
    );
    const link = screen.getByRole('link', { name: 'https://example.com/x' });
    expect(link).toHaveAttribute('href', 'https://example.com/x');
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'));
    expect(container.querySelector('.tok-timestamp')).toHaveTextContent('2026-11-14T12:00:00Z');
    expect(container.querySelector('.tok-logger')).toHaveTextContent('[main]');
    expect(container.querySelector('.tok-uuid')).toHaveTextContent('123e4567-e89b-12d3-a456-426614174000');
  });

  it('never interprets log text as markup', () => {
    const { container } = render(<LogText text={'<img src=x onerror=alert(1)> <b>bold</b>'} />);
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('b')).toBeNull();
    expect(container).toHaveTextContent('<img src=x onerror=alert(1)> <b>bold</b>');
  });

  it('marks the search matches inside the text', () => {
    const { container } = render(
      <LogText text="an error happened" highlight={{ query: 'error', regex: false }} />,
    );
    expect(container.querySelector('mark')).toHaveTextContent('error');
  });
});
