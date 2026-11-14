import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { FieldFilter } from './FieldFilter';
import type { LineFilter } from './levels';

function mount(value: LineFilter = {}) {
  const onChange = vi.fn();
  render(<FieldFilter value={value} onChange={onChange} />);
  return onChange;
}

describe('FieldFilter', () => {
  it('has a checkbox with a text label for every level, all on without a filter', () => {
    mount();
    for (const level of ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL']) {
      expect(screen.getByRole('checkbox', { name: level })).toBeChecked();
    }
  });

  it('turning a level off filters by the remaining set', async () => {
    const onChange = mount();
    await userEvent.click(screen.getByRole('checkbox', { name: 'DEBUG' }));
    const next = onChange.mock.calls[0]?.[0] as LineFilter;
    expect([...(next.levels ?? [])].sort()).toEqual(['ERROR', 'FATAL', 'INFO', 'TRACE', 'WARN']);
  });

  it('turning every level on again removes the level filter', async () => {
    const onChange = mount({ levels: new Set(['TRACE', 'INFO', 'WARN', 'ERROR', 'FATAL']) });
    await userEvent.click(screen.getByRole('checkbox', { name: 'DEBUG' }));
    expect((onChange.mock.calls[0]?.[0] as LineFilter).levels).toBeUndefined();
  });

  it('filters by field = value', async () => {
    const onChange = mount();
    await userEvent.type(screen.getByLabelText('Field'), 'user');
    expect(onChange).toHaveBeenLastCalledWith({ field: { key: 'user', value: '' } });
  });

  it('shows the current field filter and clears it', async () => {
    const onChange = mount({ field: { key: 'user', value: 'ada' } });
    expect(screen.getByLabelText('Field')).toHaveValue('user');
    expect(screen.getByLabelText('Value')).toHaveValue('ada');
    expect(screen.getByText('Plain lines are hidden while a field filter is on.')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Clear field filter' }));
    expect(onChange).toHaveBeenCalledWith({});
  });
});
