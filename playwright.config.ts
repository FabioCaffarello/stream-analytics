import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/playwright/specs',
  outputDir: './tests/playwright/artifacts/results',
  timeout: 120_000,
  retries: 1,
  workers: 1, // serial — canvas/WASM client is single-instance
  use: {
    baseURL: 'http://localhost:8090',
    viewport: { width: 1920, height: 1080 },
    screenshot: 'only-on-failure',
    trace: 'on-first-retry',
    video: 'on-first-retry',
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
  ],
  reporter: [
    ['list'],
    ['html', { outputFolder: 'tests/playwright/artifacts/report', open: 'never' }],
    ['json', { outputFile: 'tests/playwright/artifacts/results.json' }],
  ],
});
