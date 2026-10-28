import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  workers: 2,
  reporter: 'list',
  globalSetup: './tests/development/setup.ts',
  use: {
    baseURL: 'http://127.0.0.1:5174',
    viewport: { width: 1440, height: 1080 },
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'npm run dev -- --mode fixture --port 5174',
    url: 'http://127.0.0.1:5174',
    reuseExistingServer: false,
    env: {
      SHELDON_DEV_ORIGIN: 'http://127.0.0.1:5174',
      SHELDON_BACKEND_URL: 'http://127.0.0.1:7332',
    },
  },
})
