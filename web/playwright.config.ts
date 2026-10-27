import { defineConfig, devices } from '@playwright/test'
import { resolve } from 'node:path'

/**
 * End-to-end tests run against the real Go server with a mock OIDC provider.
 * Set E2E_BASE_URL to test an already-running instance (no servers are started then).
 * E2E_PORT and E2E_OIDC_PORT move the servers this config starts, e.g. to run
 * the suite next to `make dev`, which already uses 8080 and 9999.
 */
const external = process.env.E2E_BASE_URL
const port = process.env.E2E_PORT ?? '8080'
const oidcPort = process.env.E2E_OIDC_PORT ?? '9999'
const oidcIssuer = `http://localhost:${oidcPort}`
process.env.E2E_OIDC_ISSUER ??= oidcIssuer
const baseURL = external ?? `http://localhost:${port}`
const repoRoot = resolve(import.meta.dirname, '..')
const dataDir = resolve(import.meta.dirname, 'test-results', 'e2e-data')

export default defineConfig({
  testDir: './e2e',
  outputDir: './test-results/artifacts',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  reporter: [['list'], ['html', { outputFolder: 'playwright-report', open: 'never' }]],
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    locale: 'en-US',
    timezoneId: 'America/New_York',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } }],
  webServer: external
    ? undefined
    : [
        {
          command: 'go run ./cmd/mockoidc',
          cwd: repoRoot,
          url: `${oidcIssuer}/.well-known/openid-configuration`,
          reuseExistingServer: !process.env.CI,
          timeout: 180_000,
          stdout: 'pipe',
          env: { MOCKOIDC_ADDR: `127.0.0.1:${oidcPort}`, MOCKOIDC_ISSUER: oidcIssuer },
        },
        {
          // A fresh data directory per run keeps tests independent of local state.
          command: `rm -rf "${dataDir}" && mkdir -p "${dataDir}" && go run ./cmd/sharedcal serve`,
          cwd: repoRoot,
          url: `${baseURL}/readyz`,
          reuseExistingServer: !process.env.CI,
          timeout: 180_000,
          stdout: 'pipe',
          env: {
            APP_URL: baseURL,
            LISTEN_ADDR: `:${port}`,
            DATA_DIR: dataDir,
            DB_DRIVER: 'sqlite',
            DB_DSN: resolve(dataDir, 'sharedcal.db'),
            OIDC_ISSUER: oidcIssuer,
            OIDC_CLIENT_ID: 'sharedcal',
            OIDC_CLIENT_SECRET: 'sharedcal-dev-secret',
            OIDC_ADMIN_GROUPS: 'calendar-admins',
            OIDC_WRITER_GROUPS: 'calendar-writers',
            OIDC_VIEWER_GROUPS: 'calendar-viewers',
            DEFAULT_ROLE: 'viewer',
            LOG_LEVEL: 'warn',
          },
        },
      ],
})
