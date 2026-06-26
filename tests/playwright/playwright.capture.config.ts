import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './capture',
  testMatch: /\d\d-\w.*\.ts$/,
  outputDir: './artifacts/capture-results',
  timeout: 120_000,
  retries: 0,
  workers: 1,
  use: {
    baseURL: 'http://localhost:8090',
    viewport: { width: 1920, height: 1080 },
    screenshot: 'on',
    video: {
      mode: 'on',
      size: { width: 1920, height: 1080 },
    },
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
  ],
  reporter: [['list']],
});
