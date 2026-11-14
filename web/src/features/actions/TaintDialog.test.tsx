import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { TaintDialog, type Taint } from './TaintDialog';

const taints: Taint[] = [
  { key: 'dedicated', value: 'gpu', effect: 'NoSchedule' },
  { key: 'maintenance', effect: 'NoExecute' },
];

function mount(over: Partial<Parameters<typeof TaintDialog>[0]> = {}) {
  const props = {
    nodeName: 'node-1',
    taints,
    busy: false,
    onAdd: vi.fn(),
    onRemove: vi.fn(),
    onClose: vi.fn(),
    ...over,
  };
  render(<TaintDialog {...props} />);
  return props;
}

describe('TaintDialog', () => {
  it('lists the taints of the node, with and without a value', () => {
    mount();
    expect(screen.getByRole('dialog', { name: 'Taints of node-1' })).toBeInTheDocument();
    expect(screen.getByText('dedicated=gpu:NoSchedule')).toBeInTheDocument();
    expect(screen.getByText('maintenance:NoExecute')).toBeInTheDocument();
  });

  it('says so when the node has no taints', () => {
    mount({ taints: [] });
    expect(screen.getByText('This node has no taints.')).toBeInTheDocument();
  });

  it('removes a taint by its key and effect', async () => {
    const p = mount();
    await userEvent.click(screen.getByRole('button', { name: 'Remove maintenance:NoExecute' }));
    expect(p.onRemove).toHaveBeenCalledWith({ key: 'maintenance', effect: 'NoExecute' });
  });

  it('adds a taint once it has a key; NoSchedule is the default effect', async () => {
    const p = mount();
    const add = screen.getByRole('button', { name: 'Add taint' });
    expect(add).toBeDisabled();
    await userEvent.type(screen.getByLabelText('Key'), 'team');
    await userEvent.type(screen.getByLabelText('Value (optional)'), 'ml');
    expect(add).toBeEnabled();
    await userEvent.click(add);
    expect(p.onAdd).toHaveBeenCalledWith({ key: 'team', value: 'ml', effect: 'NoSchedule' });
  });

  it('offers the three effects', async () => {
    const p = mount();
    await userEvent.type(screen.getByLabelText('Key'), 'k');
    await userEvent.selectOptions(screen.getByLabelText('Effect'), 'NoExecute');
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual([
      'NoSchedule',
      'PreferNoSchedule',
      'NoExecute',
    ]);
    await userEvent.click(screen.getByRole('button', { name: 'Add taint' }));
    expect(p.onAdd).toHaveBeenCalledWith({ key: 'k', value: '', effect: 'NoExecute' });
  });

  it('refuses a duplicate key and effect before asking the server', async () => {
    const p = mount();
    await userEvent.type(screen.getByLabelText('Key'), 'dedicated');
    expect(screen.getByText('The node already has this taint.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add taint' })).toBeDisabled();
    expect(p.onAdd).not.toHaveBeenCalled();
  });

  it('shows the error and closes with Escape or the Close button', async () => {
    const p = mount({ error: 'Conflict: try again.' });
    expect(screen.getByRole('alert')).toHaveTextContent('Conflict: try again.');
    await userEvent.keyboard('{Escape}');
    await userEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(p.onClose).toHaveBeenCalledTimes(2);
  });

  it('disables the buttons while busy', () => {
    mount({ busy: true });
    expect(screen.getByRole('button', { name: 'Remove maintenance:NoExecute' })).toBeDisabled();
  });
});
