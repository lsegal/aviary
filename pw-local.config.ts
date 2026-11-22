import { defineConfig, devices } from "@playwright/test";
export default defineConfig({
	testDir: "./web/e2e",
	reporter: "list",
	workers: 4,
	use: { baseURL: "http://localhost:5297", ignoreHTTPSErrors: true },
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
	webServer: {
		command: "npx vite --port 5297 --strictPort",
		url: "http://localhost:5297",
		reuseExistingServer: false,
		timeout: 60_000,
	},
});
