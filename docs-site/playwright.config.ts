import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './tests/browser',
  timeout: 30000,
  fullyParallel: true,
  workers: 3,
  use: {
    baseURL: process.env['RUNLOOM_DOCS_TEST_URL'] || 'http://127.0.0.1:4402',
    screenshot: 'only-on-failure',
    launchOptions: process.env['RUNLOOM_CHROME']
      ? { executablePath: process.env['RUNLOOM_CHROME'] }
      : {},
  },
  projects: [
    { name: 'desktop', use: { viewport: { width: 1440, height: 1000 } } },
    {
      name: 'mobile',
      use: { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true },
    },
  ],
  webServer: process.env['RUNLOOM_DOCS_TEST_URL']
    ? undefined
    : {
        command:
          'npx --no-install serve dist/runloom-docs/browser -l tcp://127.0.0.1:4402 --no-clipboard',
        url: 'http://127.0.0.1:4402',
        reuseExistingServer: false,
      },
});
