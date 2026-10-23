import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./integration",
  workers: 1,
  timeout: 120000,
  expect: { timeout: 15000 },
  use: {
    baseURL: process.env.STUDYVAULT_BASE_URL || "http://127.0.0.1:5175",
    viewport: { width: 1440, height: 1050 },
    trace: "off",
    screenshot: "only-on-failure",
  },
  webServer: process.env.STUDYVAULT_BASE_URL
    ? undefined
    : {
        command:
          "node node_modules/vite/bin/vite.js preview --host 127.0.0.1 --port 5175 --strictPort",
        url: "http://127.0.0.1:5175",
        reuseExistingServer: true,
      },
  reporter: "list",
});
