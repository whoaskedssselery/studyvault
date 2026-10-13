import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  fullyParallel: false,
  timeout: 30000,
  use: {
    baseURL: "http://127.0.0.1:5173",
    viewport: { width: 1440, height: 1050 },
    trace: "retain-on-failure",
  },
  webServer: [{ command: "node node_modules/vite/bin/vite.js --host 127.0.0.1 --port 5173 --strictPort", url: "http://127.0.0.1:5173", reuseExistingServer: true }, { command: "node ../deliverables/frontend/serve.mjs --port 4174", url: "http://127.0.0.1:4174", reuseExistingServer: true }],
  reporter: "list",
});
