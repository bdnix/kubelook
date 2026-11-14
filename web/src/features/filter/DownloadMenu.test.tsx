import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { parseAllDocuments } from 'yaml';
import { describe, expect, it, vi } from 'vitest';
import { DownloadMenu } from './DownloadMenu';

const objects = [
  { apiVersion: 'v1', kind: 'Pod', metadata: { name: 'a', namespace: 'ns' }, spec: { x: 1 } },
  { apiVersion: 'v1', kind: 'Pod', metadata: { name: 'b', namespace: 'ns' } },
];
const text = (bytes: Uint8Array) => new TextDecoder().decode(bytes);

function mount(over: Partial<Parameters<typeof DownloadMenu>[0]> = {}) {
  const saveFile = vi.fn();
  const clipboard = { writeText: vi.fn(() => Promise.resolve()) };
  render(
    <DownloadMenu
      cluster="prod"
      kind="Pod"
      resource="pods"
      namespace="ns"
      objects={objects}
      filter={[[{ path: 'status.phase', op: '=', value: 'Running' }]]}
      saveFile={saveFile}
      clipboard={clipboard}
      now={() => new Date('2026-11-14T12:00:05Z')}
      {...over}
    />,
  );
  return { saveFile, clipboard };
}

describe('DownloadMenu', () => {
  it('downloads the filtered objects as multi-document YAML', async () => {
    const { saveFile } = mount();
    await userEvent.click(screen.getByRole('button', { name: 'Download matches' }));
    await userEvent.click(screen.getByRole('menuitem', { name: 'YAML' }));
    const [name, bytes] = saveFile.mock.calls[0] as [string, Uint8Array];
    expect(name).toBe('prod-pods-20261114-120005.yaml');
    const docs = parseAllDocuments(text(bytes)).map((d) => d.toJS() as unknown);
    expect(docs).toEqual(objects);
    expect(text(bytes)).toContain('\n---\n');
  });

  it('downloads JSON as a List', async () => {
    const { saveFile } = mount();
    await userEvent.click(screen.getByRole('button', { name: 'Download matches' }));
    await userEvent.click(screen.getByRole('menuitem', { name: 'JSON' }));
    const [name, bytes] = saveFile.mock.calls[0] as [string, Uint8Array];
    expect(name).toBe('prod-pods-20261114-120005.json');
    expect(JSON.parse(text(bytes))).toEqual({ apiVersion: 'v1', kind: 'List', items: objects });
  });

  it('copies the kubectl commands with jq and with yq', async () => {
    const { clipboard } = mount();
    await userEvent.click(screen.getByRole('button', { name: 'Download matches' }));
    await userEvent.click(screen.getByRole('menuitem', { name: 'Copy as kubectl | jq' }));
    expect(clipboard.writeText).toHaveBeenLastCalledWith(
      `kubectl get pods -n ns -o json | jq '.items[] | select(any(.status.phase; . != null and tostring == "Running"))'`,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Download matches' }));
    await userEvent.click(screen.getByRole('menuitem', { name: 'Copy as kubectl | yq' }));
    expect(String((clipboard.writeText.mock.calls.at(-1) as unknown[])[0])).toContain('-o yaml | yq ');
  });

  it('without a filter the kubectl commands list everything', async () => {
    const { clipboard } = mount({ filter: [] });
    await userEvent.click(screen.getByRole('button', { name: 'Download matches' }));
    await userEvent.click(screen.getByRole('menuitem', { name: 'Copy as kubectl | jq' }));
    expect(clipboard.writeText).toHaveBeenLastCalledWith('kubectl get pods -n ns -o json | jq \'.items[]\'');
  });

  it('is disabled without matches, and clusters-scoped kinds have no namespace flag', async () => {
    mount({ objects: [] });
    expect(screen.getByRole('button', { name: 'Download matches' })).toBeDisabled();
  });

  it('a cluster-scoped kind leaves out the namespace', async () => {
    const { clipboard } = mount({ namespace: null, resource: 'nodes', kind: 'Node' });
    await userEvent.click(screen.getByRole('button', { name: 'Download matches' }));
    await userEvent.click(screen.getByRole('menuitem', { name: 'Copy as kubectl | jq' }));
    expect(String((clipboard.writeText.mock.calls.at(-1) as unknown[])[0])).toMatch(/^kubectl get nodes -o json/);
  });
});
