import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  testMatch: 'release.spec.ts',
  workers: 1,
  reporter: 'list',
  globalSetup: './tests/release/setup.ts',
  use: { viewport: { width: 1440, height: 1080 }, trace: 'off' },
})
