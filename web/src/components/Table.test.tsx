import { fireEvent, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import type { Column } from '../kinds/types';
import { getString } from '../lib/path';
import type { K8sObject } from '../state/live';
import { Table } from './Table';

const now = Date.parse('2026-11-14T12:00:00Z');
const columns: Column[] = [
  { id: 'name', title: 'Name', value: (o) => getString(o, 'metadata.name') },
  {
    id: 'restarts',
    title: 'Restarts',
    value: (o) => Number(getString(o, 'spec.restarts', '0')),
    sort: 'number',
  },
  { id: 'age', title: 'Age', value: (_o, n) => `${n}`, sort: 'age' },
  { id: 'status', title: 'Status', value: () => 'Running' },
];

function obj(i: number, restarts = i, created = '2026-11-14T10:00:00Z'): K8sObject {
  return {
    metadata: { uid: `uid-${i}`, name: `pod-${String(i).padStart(3, '0')}`, creationTimestamp: created },
    spec: { restarts: String(restarts) },
  };
}

const getKey = (o: K8sObject) => getString(o, 'metadata.uid');

function renderTable(rows: K8sObject[], props: Partial<React.ComponentProps<typeof Table>> = {}) {
  return render(
    <Table
      label="Pods"
      columns={columns}
      rows={rows}
      getKey={getKey}
      now={now}
      viewportHeight={96}
      rowHeight={32}
      {...props}
    />,
  );
}

const bodyRows = () => screen.getAllByRole('row').filter((r) => r.hasAttribute('aria-rowindex'));
const names = () => bodyRows().map((r) => within(r).getAllByRole('gridcell')[1]?.textContent);

describe('Table', () => {
  it('renders only the windowed rows but reports the total in aria-rowcount', () => {
    const rows = Array.from({ length: 1000 }, (_, i) => obj(i));
    renderTable(rows);
    expect(screen.getByRole('grid', { name: 'Pods' })).toHaveAttribute('aria-rowcount', '1000');
    expect(bodyRows().length).toBeLessThan(20);
    expect(names()[0]).toBe('pod-000');
  });

  it('shows the rows that scroll into view', () => {
    const rows = Array.from({ length: 1000 }, (_, i) => obj(i));
    const { container } = renderTable(rows);
    const body = container.querySelector('.grid-body') as HTMLElement;
    fireEvent.scroll(body, { target: { scrollTop: 32 * 500 } });
    expect(names()).toContain('pod-500');
    expect(names()).not.toContain('pod-000');
  });

  it('sorts when a header is clicked: ascending, descending, off', async () => {
    const rows = [obj(1, 5), obj(2, 1), obj(3, 3)];
    renderTable(rows);
    const header = screen.getByRole('columnheader', { name: 'Restarts' });
    expect(header).toHaveAttribute('aria-sort', 'none');
    await userEvent.click(header);
    expect(header).toHaveAttribute('aria-sort', 'ascending');
    expect(names()).toEqual(['pod-002', 'pod-003', 'pod-001']);
    await userEvent.click(header);
    expect(header).toHaveAttribute('aria-sort', 'descending');
    expect(names()).toEqual(['pod-001', 'pod-003', 'pod-002']);
    await userEvent.click(header);
    expect(header).toHaveAttribute('aria-sort', 'none');
    expect(names()).toEqual(['pod-001', 'pod-002', 'pod-003']);
  });

  it('sorts text and age columns', async () => {
    const rows = [obj(1, 0, '2026-11-14T08:00:00Z'), obj(2, 0, '2026-11-14T11:00:00Z'), obj(3, 0, 'bad')];
    renderTable(rows);
    await userEvent.click(screen.getByRole('columnheader', { name: 'Age' }));
    expect(names()).toEqual(['pod-002', 'pod-001', 'pod-003'].sort((a, b) => 0 * a.length - 0 * b.length));
    await userEvent.click(screen.getByRole('columnheader', { name: 'Name' }));
    await userEvent.click(screen.getByRole('columnheader', { name: 'Name' }));
    expect(names()).toEqual(['pod-003', 'pod-002', 'pod-001']);
  });

  it('filters by text in any column', async () => {
    renderTable([obj(1), obj(2), obj(13)]);
    await userEvent.type(screen.getByRole('searchbox', { name: /Filter Pods/ }), 'POD-01');
    expect(names()).toEqual(['pod-013'].map((n) => n));
    expect(screen.getByRole('grid')).toHaveAttribute('aria-rowcount', '1');
  });

  it('selects rows with checkboxes and a shift-click range', async () => {
    const onSelectionChange = vi.fn();
    const user = userEvent.setup();
    renderTable([obj(1), obj(2), obj(3)], { onSelectionChange, viewportHeight: 200 });
    await user.click(screen.getByRole('checkbox', { name: 'Select pod-001' }));
    expect(onSelectionChange).toHaveBeenLastCalledWith(['uid-1']);
    await user.keyboard('{Shift>}');
    await user.click(screen.getByRole('checkbox', { name: 'Select pod-003' }));
    await user.keyboard('{/Shift}');
    expect(onSelectionChange).toHaveBeenLastCalledWith(['uid-1', 'uid-2', 'uid-3']);
    await user.click(screen.getByRole('checkbox', { name: 'Select pod-002' }));
    expect(onSelectionChange).toHaveBeenLastCalledWith(['uid-1', 'uid-3']);
    expect(screen.getByRole('checkbox', { name: 'Select pod-002' })).not.toBeChecked();
  });

  it('moves the active row with the arrow keys and opens it with Enter', async () => {
    const onOpen = vi.fn();
    renderTable(
      Array.from({ length: 10 }, (_, i) => obj(i)),
      { onOpen },
    );
    const grid = screen.getByRole('grid');
    grid.focus();
    await userEvent.keyboard('{ArrowDown}{ArrowDown}');
    expect(grid.getAttribute('aria-activedescendant')).toBe('Pods-row-2');
    await userEvent.keyboard('{ArrowUp}{Enter}');
    expect(onOpen).toHaveBeenCalledWith(
      expect.objectContaining({ metadata: expect.objectContaining({ name: 'pod-001' }) }),
    );
    await userEvent.keyboard('{End}');
    expect(grid.getAttribute('aria-activedescendant')).toBe('Pods-row-9');
    expect(names()).toContain('pod-009');
    await userEvent.keyboard('{ArrowDown}');
    expect(grid.getAttribute('aria-activedescendant')).toBe('Pods-row-9');
    await userEvent.keyboard('{Home}');
    expect(grid.getAttribute('aria-activedescendant')).toBe('Pods-row-0');
    expect(names()).toContain('pod-000');
  });

  it('Space toggles the active row; double click opens a row', async () => {
    const onOpen = vi.fn();
    const onSelectionChange = vi.fn();
    renderTable([obj(1), obj(2)], { onOpen, onSelectionChange });
    screen.getByRole('grid').focus();
    await userEvent.keyboard(' ');
    expect(onSelectionChange).toHaveBeenLastCalledWith(['uid-1']);
    await userEvent.dblClick(bodyRows()[1] as HTMLElement);
    expect(onOpen).toHaveBeenCalledWith(
      expect.objectContaining({ metadata: expect.objectContaining({ name: 'pod-002' }) }),
    );
    await userEvent.click(bodyRows()[1] as HTMLElement);
    expect(bodyRows()[1]).toHaveClass('active');
  });

  it('shows the status column as a badge when a status function is given', () => {
    renderTable([obj(1)], { statusOf: () => ({ text: 'Running', severity: 'ok' }) });
    expect(screen.getByRole('img', { name: 'healthy' })).toBeInTheDocument();
  });

  it('is empty without rows and ignores keys', async () => {
    renderTable([]);
    const grid = screen.getByRole('grid');
    expect(grid).toHaveAttribute('aria-rowcount', '0');
    expect(grid).not.toHaveAttribute('aria-activedescendant');
    grid.focus();
    await userEvent.keyboard('{ArrowDown}{Enter} ');
    expect(bodyRows()).toHaveLength(0);
  });

  it('measures its height with a ResizeObserver when none is given', () => {
    const observe = vi.fn();
    const disconnect = vi.fn();
    class FakeObserver {
      constructor(public cb: () => void) {
        setTimeout(cb, 0);
      }
      observe = observe;
      disconnect = disconnect;
    }
    vi.stubGlobal('ResizeObserver', FakeObserver);
    const { unmount } = render(
      <Table label="Pods" columns={columns} rows={[obj(1)]} getKey={getKey} now={now} />,
    );
    expect(observe).toHaveBeenCalled();
    unmount();
    expect(disconnect).toHaveBeenCalled();
    vi.unstubAllGlobals();
  });
});
