import { expect, test } from '@playwright/test';
import { cluster, pod, setup } from './fixtures';
import { installGuard } from './guard';

installGuard();

const items = (d: Record<string, unknown>) => (d.resource === 'pods' ? [pod('1', 'web-1', 'Running')] : []);
const base64 = (s: string) => Buffer.from(s).toString('base64');

test('Open shell opens a terminal tab: output shows, typing is sent, and a closed shell offers a new one', async ({
  page,
}) => {
  const socket = await setup(
    page,
    {},
    {
      items,
      extra: (msg, send) => {
        if (msg.op === 'exec.open') {
          send({ ch: msg.ch, t: 'opened' });
          send({ ch: msg.ch, t: 'opened', d: { shell: '/bin/sh' } });
          send({ ch: msg.ch, t: 'out', d: { data: base64('hello from the shell\r\n$ ') } });
        }
      },
    },
  );
  await page.goto(`/c/${cluster.path}/ns/shop/core/Pod/web-1`);
  await page.getByRole('button', { name: 'Open shell' }).click();

  const dock = page.getByRole('region', { name: 'Dock' });
  await expect(dock.getByRole('tab', { name: /Shell/ })).toBeVisible();
  await expect(dock.locator('.xterm')).toContainText('hello from the shell');

  await dock.locator('.xterm').click();
  await page.keyboard.type('ls\n');
  await expect
    .poll(() =>
      socket.received
        .filter((m) => m.op === 'exec.stdin')
        .map((m) => Buffer.from(String((m.d as { data: string }).data), 'base64').toString())
        .join(''),
    )
    .toContain('ls');

  const ch = socket.received.find((m) => m.op === 'exec.open')?.ch ?? 0;
  socket.push({ ch, t: 'closed' });
  await expect(dock.getByRole('button', { name: 'Start a new shell' })).toBeVisible();
});
