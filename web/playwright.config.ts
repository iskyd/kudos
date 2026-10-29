import { defineConfig, devices } from '@playwright/test'

// The end-to-end tests run the real server (embedding a fresh build) against DATABASE_URL.
// `just e2e` starts a throwaway database and sets it.
const baseURL = 'http://127.0.0.1:4173'
const fakeGarminURL = 'http://127.0.0.1:4174'

export default defineConfig({
  testDir: './e2e',
  forbidOnly: !!process.env.CI,
  use: { baseURL, trace: 'on-first-retry' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      // Serves the recorded Garmin fixtures: no test calls Garmin.
      command: 'node e2e/fake-garmin.mjs',
      url: `${fakeGarminURL}/healthz`,
      reuseExistingServer: !process.env.CI,
    },
    {
      command: 'npm run build && go run ../cmd/server',
      url: `${baseURL}/healthz`,
      reuseExistingServer: !process.env.CI,
      timeout: 180_000,
      env: {
        LISTEN_ADDR: '127.0.0.1:4173',
        DATA_DIR: 'test-results/data',
        // A fixed test-only key: the e2e database holds no real secrets.
        ENCRYPTION_KEY: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=',
        REGISTRATION_ENABLED: 'true',
        GARMIN_API_URL: fakeGarminURL,
      },
    },
  ],
})
