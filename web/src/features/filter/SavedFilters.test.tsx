import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { SavedFilters } from './SavedFilters';

const sets = [
  { name: 'Failing', kind: 'Pod', query: 'status.phase = Failed' },
  { name: 'Old', kind: 'Pod', query: 'metadata.creationTimestamp older than 7d' },
  { name: 'NotReady', kind: 'Node', query: 'status.conditions[*].status = False' },
];

function mount(query = 'a = 1') {
  const handlers = { onLoad: vi.fn(), onSave: vi.fn(), onRename: vi.fn(), onDelete: vi.fn() };
  render(<SavedFilters kind="Pod" sets={sets} query={query} {...handlers} />);
  return handlers;
}

describe('SavedFilters', () => {
  it('lists only the filter sets of the kind', () => {
    mount();
    const list = screen.getByRole('list', { name: 'Saved filters' });
    expect(within(list).getAllByRole('listitem').map((i) => i.textContent)).toEqual(
      expect.arrayContaining([expect.stringContaining('Failing'), expect.stringContaining('Old')]),
    );
    expect(within(list).queryByText(/NotReady/)).toBeNull();
  });

  it('loads a set', async () => {
    const h = mount();
    await userEvent.click(screen.getByRole('button', { name: 'Load Failing' }));
    expect(h.onLoad).toHaveBeenCalledWith('status.phase = Failed');
  });

  it('saves the current query under a name', async () => {
    const h = mount('metadata.name contains web');
    await userEvent.type(screen.getByLabelText('Name'), 'Web pods');
    await userEvent.click(screen.getByRole('button', { name: 'Save filter' }));
    expect(h.onSave).toHaveBeenCalledWith({ name: 'Web pods', kind: 'Pod', query: 'metadata.name contains web' });
  });

  it('cannot save an empty query, a name that is empty, or a name that is taken', async () => {
    mount('');
    expect(screen.getByRole('button', { name: 'Save filter' })).toBeDisabled();
    const h = mount('a = 1');
    const save = screen.getAllByRole('button', { name: 'Save filter' })[1] as HTMLElement;
    expect(save).toBeDisabled();
    await userEvent.type(screen.getAllByLabelText('Name')[1] as HTMLElement, 'Failing');
    expect(screen.getByText('A filter with this name exists.')).toBeInTheDocument();
    expect(save).toBeDisabled();
    expect(h.onSave).not.toHaveBeenCalled();
  });

  it('renames a set', async () => {
    const h = mount();
    await userEvent.click(screen.getByRole('button', { name: 'Rename Old' }));
    const input = screen.getByLabelText('New name');
    await userEvent.clear(input);
    await userEvent.type(input, 'Ancient');
    await userEvent.click(screen.getByRole('button', { name: 'Rename' }));
    expect(h.onRename).toHaveBeenCalledWith('Old', 'Ancient');
  });

  it('deletes a set', async () => {
    const h = mount();
    await userEvent.click(screen.getByRole('button', { name: 'Delete Failing' }));
    expect(h.onDelete).toHaveBeenCalledWith('Failing');
  });
});
