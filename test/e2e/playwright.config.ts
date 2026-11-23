import { defineConfig } from "@playwright/test";

// Browser end-to-end tests of the real stack (Step 7.10): the Kubelook container of `make it-up`
// at http://localhost:18080, real Vault, real kind clusters, the identity stand-in.
export default defineConfig({
  testDir: "tests",
  testMatch: "*.spec.ts",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: !!process.env["CI"],
  reporter: process.env["CI"] ? [["github"], ["list"]] : "list",
  use: {
    baseURL: "http://localhost:18080",
    trace: "retain-on-failure",
    launchOptions: {
      // The browser resolves the identity stand-in's name to the port the container publishes on this host.
      args: ["--host-resolver-rules=MAP kubelook-it-fakeidp 127.0.0.1"],
      // Set where Chromium is installed outside Playwright's own cache.
      executablePath:
        process.env["PLAYWRIGHT_CHROMIUM_EXECUTABLE"] || undefined,
    },
  },
  expect: { timeout: 15_000 },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
});
