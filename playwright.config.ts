import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/playwright',
  timeout: 120_000,
  use: {
    baseURL: 'http://localhost:8090',
    viewport: { width: 1920, height: 1080 },
    screenshot: 'on',
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
  ],
});
