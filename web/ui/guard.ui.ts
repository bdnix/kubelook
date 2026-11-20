import { expect, test } from '@playwright/test';
import { installGuard, takeProblems } from './guard';

installGuard();

test('the guard collects an uncaught error of the page and a Content Security Policy message', async ({
  page,
}) => {
  await page.route('**/guard-probe', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: '<script>setTimeout(() => { throw new Error("probe error"); }, 0); console.error("Content Security Policy probe");</script>',
    }),
  );
  await page.goto('/guard-probe');
  const seen: string[] = [];
  await expect
    .poll(() => {
      seen.push(...takeProblems(page));
      return seen.length;
    })
    .toBe(2);
  expect(seen.join('\n')).toContain('probe error');
  expect(seen.join('\n')).toContain('Content Security Policy');
});
