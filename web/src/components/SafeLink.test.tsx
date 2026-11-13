import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { SafeLink } from './SafeLink';

describe('SafeLink', () => {
  it.each(['https://a.b', 'http://a.b/path?q=1'])('renders an anchor for %s', (href) => {
    render(<SafeLink href={href}>go</SafeLink>);
    const a = screen.getByRole('link', { name: 'go' });
    expect(a).toHaveAttribute('href', href);
    expect(a).toHaveAttribute('rel', 'noopener noreferrer');
    expect(a).toHaveAttribute('target', '_blank');
  });

  it.each([
    'javascript:alert(1)',
    'JaVaScRiPt:alert(1)',
    'data:text/html,<script>1</script>',
    'ftp://a.b',
    '/relative/path',
    'not a url',
    '',
    'https://',
  ])('renders %j as plain text without an anchor', (href) => {
    render(<SafeLink href={href}>label</SafeLink>);
    expect(screen.queryByRole('link')).toBeNull();
    expect(screen.getByText('label')).toBeInTheDocument();
  });
});
