import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
	testDir: "./web/e2e",
	outputDir: "./test-results",
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	reporter: "list",
	use: {
		baseURL: process.env.CI
			? "https://localhost:16677"
			: "http://localhost:5173",
		ignoreHTTPSErrors: true,
		trace: "on-first-retry",
	},
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
	webServer: {
		command: process.env.CI ? "./aviary serve" : "pnpm dev",
		url: process.env.CI ? "https://localhost:16677" : "http://localhost:5173",
		ignoreHTTPSErrors: true,
		reuseExistingServer: !process.env.CI,
		timeout: 30_000,
	},
});
