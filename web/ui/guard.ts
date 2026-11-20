import { expect, test, type Page } from '@playwright/test';

const collected = new WeakMap<Page, string[]>();

/**
 * Collects what a page must never do: uncaught errors, console messages about the Content Security
 * Policy, and `securitypolicyviolation` events. Call it before the first navigation.
 */
export function guardPage(page: Page): void {
  const problems: string[] = [];
  collected.set(page, problems);
  page.on('pageerror', (e) => problems.push(`page error: ${e.message}`));
  page.on('console', (m) => {
    if (m.text().includes('Content Security Policy')) problems.push(`console: ${m.text()}`);
  });
  void page.addInitScript(() => {
    document.addEventListener('securitypolicyviolation', (e) => {
      // reported on the console, which the listener above collects
      console.error(`Content Security Policy violation: ${e.violatedDirective} blocked ${e.blockedURI}`);
    });
  });
}

/** What the guard collected so far, emptied by this call. A test of the guard itself uses it. */
export function takeProblems(page: Page): string[] {
  const list = collected.get(page) ?? [];
  return list.splice(0, list.length);
}

/** Guards every test of the file and fails the test when the guard collected anything. */
export function installGuard(): void {
  test.beforeEach(({ page }) => guardPage(page));
  test.afterEach(({ page }) => {
    expect(collected.get(page) ?? [], 'page errors and Content Security Policy violations').toEqual([]);
  });
}
