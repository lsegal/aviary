import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
	testDir: "./web/e2e",
	outputDir: "./test-results",
	// Writes the merged coverage report when AVIARY_E2E_COVERAGE is set.
	globalTeardown: "./web/e2e/helpers/coverageTeardown.ts",
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	reporter: "list",
	use: {
		baseURL: process.env.CI
			? "https://localhost:16677"
			: "https://localhost:5173",
		ignoreHTTPSErrors: true,
		trace: "on-first-retry",
	},
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
	webServer: {
		command: process.env.CI ? "./aviary serve" : "pnpm dev",
		url: process.env.CI ? "https://localhost:16677" : "https://localhost:5173",
		ignoreHTTPSErrors: true,
		reuseExistingServer: !process.env.CI,
		timeout: 30_000,
	},
});
