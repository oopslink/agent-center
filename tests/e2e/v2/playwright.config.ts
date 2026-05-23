import { defineConfig, devices } from "@playwright/test";

// agent-center v2 e2e (P12 M3). Two projects so the same scaffold
// can run on darwin local AND linux CI without changing the suite.
// On darwin, chromium-linux is grep'd out at config-time so it
// never spawns a non-existent browser path; on linux the inverse.

const isLinux = process.platform === "linux";

export default defineConfig({
  testDir: "./tests",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 2,
  reporter: [
    ["list"],
    ["html", { outputFolder: "artifacts/playwright-report", open: "never" }],
  ],
  use: {
    actionTimeout: 10_000,
    navigationTimeout: 15_000,
    trace: "on-first-retry",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
  },
  outputDir: "artifacts/test-results",
  projects: [
    {
      name: "chromium-mac",
      testIgnore: isLinux ? /.*/ : undefined,
      use: { ...devices["Desktop Chrome"] },
    },
    {
      name: "chromium-linux",
      testIgnore: isLinux ? undefined : /.*/,
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
