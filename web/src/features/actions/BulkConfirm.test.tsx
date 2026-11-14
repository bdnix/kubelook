import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { BulkConfirm, type BulkConfirmProps } from './BulkConfirm';

function mount(over: Partial<BulkConfirmProps> = {}) {
  const props: BulkConfirmProps = {
    action: 'delete',
    names: ['ns/a', 'ns/b', 'other/c'],
    kind: 'Pod',
    busy: false,
    onConfirm: vi.fn(),
    onCancel: vi.fn(),
    clipboard: { writeText: vi.fn(() => Promise.resolve()) },
    ...over,
  };
  render(<BulkConfirm {...props} />);
  return props;
}

describe('BulkConfirm', () => {
  it('lists every object by name and waits for an explicit confirm', async () => {
    const p = mount();
    const dialog = screen.getByRole('dialog', { name: 'Delete 3 Pods' });
    const items = within(dialog).getAllByRole('listitem');
    expect(items.map((i) => i.textContent)).toEqual(['ns/a', 'ns/b', 'other/c']);
    expect(p.onConfirm).not.toHaveBeenCalled();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Delete 3 objects' }));
    expect(p.onConfirm).toHaveBeenCalledWith(undefined);
  });

  it('is a modal that Escape cancels, and Cancel too', async () => {
    const p = mount();
    expect(screen.getByRole('dialog')).toHaveAttribute('aria-modal', 'true');
    await userEvent.keyboard('{Escape}');
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(p.onCancel).toHaveBeenCalledTimes(2);
  });

  it('a long list scrolls instead of being cut off', () => {
    const names = Array.from({ length: 500 }, (_, i) => `ns/pod-${i}`);
    mount({ names });
    expect(screen.getAllByRole('listitem')).toHaveLength(500);
  });

  it('drain says that emptyDir data is lost, and that the nodes are cordoned', () => {
    mount({ action: 'drain', kind: 'Node', names: ['node-1', 'node-2'] });
    expect(screen.getByRole('dialog', { name: 'Drain 2 Nodes' })).toBeInTheDocument();
    expect(screen.getByText(/emptyDir volumes are evicted too/)).toBeInTheDocument();
    expect(screen.getByText(/one node at a time/)).toBeInTheDocument();
    expect(screen.getByText(/cordoned first/)).toBeInTheDocument();
  });

  it('shows the kubectl equivalent as a command per object', async () => {
    const p = mount({ action: 'delete', kind: 'Pod', names: ['ns/a'] });
    expect(screen.getByText('kubectl delete pod a -n ns')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Copy as kubectl' }));
    expect(p.clipboard?.writeText).toHaveBeenCalledWith('kubectl delete pod a -n ns');
  });

  it('label needs something to add or remove and passes both to the server', async () => {
    const p = mount({ action: 'label', kind: 'Pod', names: ['ns/a'] });
    const confirm = screen.getByRole('button', { name: 'Label 1 object' });
    expect(confirm).toBeDisabled();
    await userEvent.type(screen.getByLabelText('Labels to add'), 'env=prod, tier=web');
    await userEvent.type(screen.getByLabelText('Labels to remove'), 'old');
    expect(confirm).toBeEnabled();
    await userEvent.click(confirm);
    expect(p.onConfirm).toHaveBeenCalledWith({ add: { env: 'prod', tier: 'web' }, remove: ['old'] });
  });

  it('label rejects entries without an equals sign', async () => {
    mount({ action: 'label', kind: 'Pod', names: ['ns/a'] });
    await userEvent.type(screen.getByLabelText('Labels to add'), 'novalue');
    expect(screen.getByText('Write labels as key=value, separated by commas.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Label 1 object' })).toBeDisabled();
  });

  it('shows an error and disables confirm while busy', () => {
    mount({ error: 'Too many.', busy: true });
    expect(screen.getByRole('alert')).toHaveTextContent('Too many.');
    expect(screen.getByRole('button', { name: 'Delete 3 objects' })).toBeDisabled();
  });

  it('uses the verbs of each action', () => {
    for (const [action, title] of [
      ['restart', 'Restart 3 Pods'],
      ['cordon', 'Cordon 3 Pods'],
      ['uncordon', 'Uncordon 3 Pods'],
    ] as const) {
      const { unmount } = render(
        <BulkConfirm
          action={action}
          names={['a', 'b', 'c']}
          kind="Pod"
          busy={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByRole('dialog', { name: title })).toBeInTheDocument();
      unmount();
    }
  });
});
