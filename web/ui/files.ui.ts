import { expect, test } from '@playwright/test';
import { cluster, pod, setup } from './fixtures';
import { installGuard } from './guard';
import { editorText } from './helpers';

installGuard();

// The real Monaco editor opens text files; its worker may report problems of its own for a plain text model.
const items = (d: Record<string, unknown>) => (d.resource === 'pods' ? [pod('1', 'web-1', 'Running')] : []);
const base64 = (s: string) => Buffer.from(s).toString('base64');

test('the Files tab lists a directory, opens a text file in the editor and shows the conflict message', async ({
  page,
}) => {
  await setup(
    page,
    {},
    {
      items,
      extra: (msg, send) => {
        const rid = (msg.d as { rid?: string } | undefined)?.rid;
        if (msg.op === 'file.open') send({ ch: msg.ch, t: 'opened' });
        if (msg.op === 'file.list') {
          send({
            ch: msg.ch,
            t: 'list',
            d: {
              rid,
              skipped: 0,
              entries: [
                {
                  name: 'notes.txt',
                  type: 'file',
                  mode: '-rw-r--r--',
                  size: 11,
                  modified: '2026-11-20 00:00',
                },
                { name: 'logs', type: 'dir', mode: 'drwxr-xr-x', size: 0, modified: '2026-11-20 00:00' },
              ],
            },
          });
        }
        if (msg.op === 'file.read') {
          send({
            ch: msg.ch,
            t: 'content',
            d: {
              rid,
              data: base64('hello files'),
              binary: false,
              truncated: false,
              stat: { size: 11, modified: '2026-11-20 00:00' },
            },
          });
        }
        if (msg.op === 'file.save') {
          send({
            ch: msg.ch,
            t: 'error',
            d: { rid, code: 'conflict', message: 'The file changed in the container after you opened it.' },
          });
        }
      },
    },
  );
  await page.goto(`/c/${cluster.path}/ns/shop/core/Pod/web-1`);
  const drawer = page.getByRole('complementary', { name: 'Details' });
  await drawer.getByRole('tab', { name: 'Files' }).click();

  const list = drawer.getByRole('table', { name: 'Files' });
  await expect(list.getByRole('button', { name: 'notes.txt', exact: true })).toBeVisible();
  await expect(list.getByRole('button', { name: 'logs', exact: true })).toBeVisible();

  await list.getByRole('button', { name: 'notes.txt', exact: true }).click();
  await expect.poll(() => editorText(page)).toContain('hello files');

  await page.locator('.monaco-editor').first().click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type(' more');
  await drawer.getByRole('button', { name: 'Save' }).click();
  await expect(drawer.getByText('The file changed in the container after you opened it.')).toBeVisible();
});
