import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  timeout: 30000,
  fullyParallel: true,
  use: {
    baseURL: "http://127.0.0.1:4302",
    launchOptions: process.env["RUNLOOM_CHROME"]
      ? { executablePath: process.env["RUNLOOM_CHROME"] }
      : {},
    screenshot: "only-on-failure",
  },
  projects: [
    { name: "desktop", use: { viewport: { width: 1440, height: 1000 } } },
    {
      name: "mobile",
      use: {
        viewport: { width: 390, height: 844 },
        isMobile: true,
        hasTouch: true,
      },
    },
  ],
  webServer: {
    command: "npm run preview -- --port 4302",
    url: "http://127.0.0.1:4302",
    reuseExistingServer: false,
  },
});
