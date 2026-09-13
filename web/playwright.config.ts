import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  timeout: 60000,
  expect: { timeout: 10000 },
  use: {
    browserName: "chromium",
    viewport: { width: 1280, height: 900 },
    reducedMotion: "reduce",
    screenshot: "only-on-failure",
  },
  reporter: "list",
});
