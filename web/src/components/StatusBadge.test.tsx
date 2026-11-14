import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { Severity } from '../kinds/types';
import { StatusBadge } from './StatusBadge';

describe('StatusBadge', () => {
  it.each<[Severity, string]>([
    ['ok', 'healthy'],
    ['warning', 'needs attention'],
    ['error', 'failing'],
    ['unknown', 'unknown'],
  ])('shows the text and an icon labelled for %s', (severity, label) => {
    const { container } = render(<StatusBadge status={{ text: 'CrashLoopBackOff', severity }} />);
    expect(screen.getByText(/CrashLoopBackOff/)).toBeInTheDocument();
    expect(screen.getByRole('img', { name: label })).toBeInTheDocument();
    expect(container.firstElementChild).toHaveClass(`badge-${severity}`);
  });
});
