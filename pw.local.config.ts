import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
	testDir: "./web/e2e",
	outputDir: "./test-results",
	fullyParallel: true,
	reporter: "list",
	use: { baseURL: "http://localhost:5199", ignoreHTTPSErrors: true },
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
	webServer: {
		command: "pnpm exec vite --port 5199 --strictPort",
		url: "http://localhost:5199",
		reuseExistingServer: false,
		timeout: 60_000,
	},
});
