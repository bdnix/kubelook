import { expect, test } from '@playwright/test';
import { json, mockApi, unauthenticated } from './helpers';

test.describe('signed out', () => {
  test.beforeEach(async ({ page }) => {
    await mockApi(page, {
      'GET /me': (r) => r.fulfill(unauthenticated),
      'GET /auth/providers': (r) => r.fulfill(json({ providers: ['github', 'google'] })),
    });
  });

  test('offers one sign-in link per configured provider', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: 'Kubelook' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Sign in with Google' })).toHaveAttribute(
      'href',
      '/auth/google/login',
    );
    await expect(page.getByRole('link', { name: 'Sign in with GitHub' })).toHaveAttribute(
      'href',
      '/auth/github/login',
    );
  });

  test('every address shows the sign-in page, not a blank screen', async ({ page }) => {
    await page.goto('/c/k%3Aprod%2Fctx/ns/default');
    await expect(page.getByRole('link', { name: 'Sign in with Google' })).toBeVisible();
  });

  test('shows a fixed message for a failed sign-in and never echoes the query text', async ({ page }) => {
    await page.goto('/?signin_error=not_allowed');
    await expect(page.getByRole('alert')).toHaveText("This account isn't allowed to sign in.");
    await page.goto('/?signin_error=%3Cb%3Eboom%3C%2Fb%3E');
    await expect(page.getByRole('link', { name: 'Sign in with Google' })).toBeVisible();
    await expect(page.getByText('boom')).toHaveCount(0);
  });
});
