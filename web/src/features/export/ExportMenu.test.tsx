import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import type { Column } from '../../kinds/types';
import { ExportMenu, exportFileName } from './ExportMenu';

const columns: Column[] = [
  { id: 'name', title: 'Name', value: (o) => String((o.metadata as { name: string }).name) },
  { id: 'phase', title: 'Phase', value: (o) => String((o.status as { phase: string }).phase) },
  { id: 'cpu', title: 'CPU', value: () => 250 },
];
const rows = [
  { apiVersion: 'v1', kind: 'Pod', metadata: { name: 'web-2' }, status: { phase: 'Pending' } },
  { apiVersion: 'v1', kind: 'Pod', metadata: { name: '=evil()' }, status: { phase: 'Running' } },
];
const text = (b: Uint8Array) => new TextDecoder().decode(b);

function mount(over: Partial<Parameters<typeof ExportMenu>[0]> = {}) {
  const saveFile = vi.fn();
  render(
    <ExportMenu
      cluster="v:prod"
      kind="pods"
      columns={columns}
      rows={rows}
      now={1}
      clock={() => new Date('2026-11-14T12:00:05Z')}
      saveFile={saveFile}
      {...over}
    />,
  );
  return saveFile;
}

async function choose(item: string) {
  await userEvent.click(screen.getByRole('button', { name: 'Export' }));
  await userEvent.click(screen.getByRole('menuitem', { name: item }));
}

describe('ExportMenu', () => {
  it('exports the visible columns of the rows it is given, in their order', async () => {
    const saveFile = mount();
    await choose('CSV');
    const [name, bytes] = saveFile.mock.calls[0] as [string, Uint8Array];
    expect(name).toBe('v_prod-pods-20261114-120005.csv');
    expect(text(bytes)).toBe("﻿Name,Phase,CPU\r\nweb-2,Pending,250\r\n'=evil(),Running,250\r\n");
  });

  it('exports the objects as JSON', async () => {
    const saveFile = mount();
    await choose('JSON');
    const [name, bytes] = saveFile.mock.calls[0] as [string, Uint8Array];
    expect(name).toBe('v_prod-pods-20261114-120005.json');
    expect(JSON.parse(text(bytes))).toEqual({ apiVersion: 'v1', kind: 'List', items: rows });
  });

  it('never carries Secret values: only what the list holds, which is masked', async () => {
    const secret = {
      apiVersion: 'v1',
      kind: 'Secret',
      metadata: { name: 'db' },
      type: 'Opaque',
      data: { password: '••••••••' },
    };
    const saveFile = mount({
      kind: 'secrets',
      rows: [secret],
      columns: [
        { id: 'name', title: 'Name', value: (o) => String((o.metadata as { name: string }).name) },
        { id: 'type', title: 'Type', value: (o) => String(o.type) },
      ],
    });
    await choose('JSON');
    await userEvent.click(screen.getByRole('button', { name: 'Export' }));
    await userEvent.click(screen.getByRole('menuitem', { name: 'CSV' }));
    for (const call of saveFile.mock.calls as Array<[string, Uint8Array]>) {
      expect(text(call[1])).not.toContain('c3VwZXJzZWNyZXQ=');
      expect(text(call[1])).not.toMatch(/password.*[A-Za-z0-9+/]{12,}/);
    }
    expect(text((saveFile.mock.calls[1] as [string, Uint8Array])[1])).toBe('﻿Name,Type\r\ndb,Opaque\r\n');
  });

  it('closes the menu after a choice, and is disabled with nothing to export', async () => {
    mount();
    await choose('CSV');
    expect(screen.queryByRole('menu')).toBeNull();
    mount({ rows: [] });
    expect(screen.getAllByRole('button', { name: 'Export' })[1]).toBeDisabled();
  });

  it('passes the current time to the columns, so age columns match what is shown', async () => {
    const saveFile = mount({
      now: 42,
      columns: [{ id: 'age', title: 'Age', value: (_o, now) => now }],
    });
    await choose('CSV');
    expect(text((saveFile.mock.calls[0] as [string, Uint8Array])[1])).toContain('\r\n42\r\n');
  });
});

describe('exportFileName', () => {
  it('is cluster, kind and the time of the injected clock, with unsafe characters replaced', () => {
    expect(exportFileName('k:a/b ctx', 'pods', new Date('2026-01-02T03:04:05Z'), 'csv')).toBe(
      'k_a_b_ctx-pods-20260102-030405.csv',
    );
    expect(exportFileName('prod', 'deployments.apps', new Date('2026-12-31T23:59:59Z'), 'json')).toBe(
      'prod-deployments.apps-20261231-235959.json',
    );
  });
});
