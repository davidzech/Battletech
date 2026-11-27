// The browser scripts' tests. They run against a site that Go serves with the fake MekBay (TestBrowser in internal/site/browser_test.go), which passes its
// address in BASE_URL: run them with BROWSER_TESTS=1 go test ./internal/site -run TestBrowser from the repo root.
const { defineConfig, devices } = require('@playwright/test');

if (!process.env.BASE_URL) {
  throw new Error('BASE_URL is not set: run the suite through Go (BROWSER_TESTS=1 go test ./internal/site -run TestBrowser)');
}

module.exports = defineConfig({
  testDir: './tests',
  // The fake MekBay is one hangar shared by every account, so the tests take turns.
  workers: 1,
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  timeout: 30000,
  expect: { timeout: 5000 },
  reporter: process.env.CI ? [['list'], ['github']] : 'list',
  use: {
    baseURL: process.env.BASE_URL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure'
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }]
});
