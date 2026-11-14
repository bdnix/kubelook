import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { FilterBar, type FilterBarProps } from './FilterBar';

const pods = [
  {
    metadata: { name: 'web-1', labels: { app: 'web' } },
    spec: { containers: [{ image: 'nginx' }] },
    status: { phase: 'Running' },
  },
];

function Harness({ initial = '', onChange, ...props }: Partial<FilterBarProps> & { initial?: string }) {
  const [value, setValue] = useState(initial);
  return (
    <FilterBar
      kind={{ group: '', kind: 'Pod' }}
      objects={pods}
      {...props}
      value={value}
      onChange={(q) => {
        setValue(q);
        onChange?.(q);
      }}
    />
  );
}

describe('FilterBar', () => {
  it('is a labelled text box that reports every change', async () => {
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    const input = screen.getByRole('combobox', { name: 'Field filter' });
    await userEvent.type(input, 'status.phase = Running');
    expect(input).toHaveValue('status.phase = Running');
    expect(onChange).toHaveBeenLastCalledWith('status.phase = Running');
  });

  it('suggests the paths of the live objects', () => {
    const { container } = render(<Harness />);
    const options = [...container.querySelectorAll('datalist option')].map((o) => o.getAttribute('value'));
    expect(options).toContain('metadata.labels.app');
    expect(options).toContain('spec.containers[*].image');
    expect(options).toContain('status.phase');
  });

  it('shows a parse error with its position, and nothing for an empty or valid filter', async () => {
    render(<Harness />);
    const input = screen.getByRole('combobox', { name: 'Field filter' });
    expect(screen.queryByRole('alert')).toBeNull();
    await userEvent.type(input, 'status.phase =');
    expect(screen.getByRole('alert')).toHaveTextContent(/value/i);
    expect(screen.getByRole('alert')).toHaveTextContent('character 15');
    await userEvent.type(input, ' Running');
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('flags a regular expression that does not compile, and says the list is not filtered by it', () => {
    render(<Harness initial="metadata.name regex (" />);
    expect(screen.getByRole('status')).toHaveTextContent('Invalid regular expression: metadata.name regex (');
    expect(screen.getByRole('status')).toHaveTextContent('not filtered by it');
  });

  it('refuses to filter Secret values', () => {
    render(<Harness kind={{ group: '', kind: 'Secret' }} initial="data.password = x" />);
    expect(screen.getByRole('alert')).toHaveTextContent("Secret values can't be filtered.");
  });

  it('clears the filter', async () => {
    const onChange = vi.fn();
    render(<Harness initial="a = 1" onChange={onChange} />);
    await userEvent.click(screen.getByRole('button', { name: 'Clear filter' }));
    expect(screen.getByRole('combobox', { name: 'Field filter' })).toHaveValue('');
    expect(onChange).toHaveBeenLastCalledWith('');
    expect(screen.queryByRole('button', { name: 'Clear filter' })).toBeNull();
  });

  it('switches between the query and rows, which edit the same filter', async () => {
    const onChange = vi.fn();
    render(<Harness initial="status.phase = Running" onChange={onChange} />);
    await userEvent.click(screen.getByRole('button', { name: 'Edit as rows' }));
    expect(screen.queryByRole('combobox', { name: 'Field filter' })).toBeNull();
    const row = screen.getByRole('group', { name: 'Condition 1' });
    expect(within(row).getByLabelText('Field')).toHaveValue('status.phase');
    expect(within(row).getByLabelText('Operator')).toHaveValue('=');
    expect(within(row).getByLabelText('Value')).toHaveValue('Running');
    await userEvent.selectOptions(within(row).getByLabelText('Operator'), '!=');
    expect(onChange).toHaveBeenLastCalledWith('status.phase != Running');
    await userEvent.click(screen.getByRole('button', { name: 'Edit as query' }));
    expect(screen.getByRole('combobox', { name: 'Field filter' })).toHaveValue('status.phase != Running');
  });

  it('rows can add and remove conditions and join them with AND or OR', async () => {
    const onChange = vi.fn();
    render(<Harness initial="a = 1" onChange={onChange} />);
    await userEvent.click(screen.getByRole('button', { name: 'Edit as rows' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add condition' }));
    const second = screen.getByRole('group', { name: 'Condition 2' });
    await userEvent.type(within(second).getByLabelText('Field'), 'b');
    await userEvent.type(within(second).getByLabelText('Value'), '2');
    expect(onChange).toHaveBeenLastCalledWith('a = 1 AND b = 2');
    await userEvent.selectOptions(within(second).getByLabelText('Join'), 'OR');
    expect(onChange).toHaveBeenLastCalledWith('a = 1 OR b = 2');
    await userEvent.click(within(second).getByRole('button', { name: 'Remove condition 2' }));
    expect(onChange).toHaveBeenLastCalledWith('a = 1');
    expect(screen.queryByRole('group', { name: 'Condition 2' })).toBeNull();
  });

  it('exists needs no value', async () => {
    render(<Harness initial="a exists" />);
    await userEvent.click(screen.getByRole('button', { name: 'Edit as rows' }));
    expect(within(screen.getByRole('group', { name: 'Condition 1' })).queryByLabelText('Value')).toBeNull();
  });

  it('rows are unavailable while the query does not parse', async () => {
    render(<Harness initial="a =" />);
    expect(screen.getByRole('button', { name: 'Edit as rows' })).toBeDisabled();
  });
});
