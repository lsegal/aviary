import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
	testDir: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-390/web/e2e",
	outputDir: "/tmp/pw678-results",
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	reporter: "list",
	use: {
		baseURL: process.env.CI
			? "https://localhost:16677"
			: "http://localhost:5678",
		ignoreHTTPSErrors: true,
		trace: "on-first-retry",
	},
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
	webServer: {
		command: process.env.CI ? "./aviary serve" : "pnpm exec vite --port 5678 --strictPort",
		url: process.env.CI ? "https://localhost:16677" : "http://localhost:5678",
		ignoreHTTPSErrors: true,
		reuseExistingServer: true,
		timeout: 30_000,
	},
});
