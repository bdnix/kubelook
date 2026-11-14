import { defineConfig } from '@playwright/test';

// Browser UI tests: the built web app in a real Chromium, with the API answered by the tests
// (page.route). No backend, Vault or cluster is needed; the full-stack browser tests are Step 7.10.
export default defineConfig({
  testDir: 'ui',
  testMatch: '*.ui.ts',
  fullyParallel: true,
  forbidOnly: !!process.env['CI'],
  retries: 0,
  reporter: process.env['CI'] ? [['github'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: 'http://127.0.0.1:4173',
    trace: 'retain-on-failure',
    launchOptions: {
      // Set where Chromium is installed outside Playwright's own cache.
      executablePath: process.env['PLAYWRIGHT_CHROMIUM_EXECUTABLE'] || undefined,
    },
  },
  expect: { timeout: 10_000 },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
  webServer: {
    command: 'npm run build && npx vite preview --host 127.0.0.1 --port 4173 --strictPort',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: !process.env['CI'],
    timeout: 120_000,
  },
});
