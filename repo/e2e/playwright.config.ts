import { defineConfig } from '@playwright/test';

/**
 * HarborWorks Playwright configuration.
 *
 * All tests use Playwright's APIRequestContext (the `request` fixture),
 * which is pure-HTTP and requires no browser binary. This lets the tests
 * run in the Go test image with just Node.js installed — no Chromium/Firefox
 * download needed.
 *
 * Set PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 to suppress the browser download
 * warning from the playwright post-install script.
 */
export default defineConfig({
  testDir: './tests',
  timeout: 30_000,
  retries: 1,

  use: {
    // Resolved from the environment so the same tests work against any host.
    baseURL: process.env.APP_URL ?? 'http://127.0.0.1:8080',
    extraHTTPHeaders: { Accept: 'application/json' },
  },

  // A single "api" project — no browser launch required.
  projects: [{ name: 'api' }],

  reporter: [['line'], ['json', { outputFile: 'e2e-results.json' }]],
});
