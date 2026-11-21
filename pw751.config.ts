import { defineConfig, devices } from "@playwright/test";
export default defineConfig({
	testDir: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-751/web/e2e",
	outputDir: "/tmp/pw751-results",
	fullyParallel: true,
	workers: 4,
	reporter: "list",
	use: { baseURL: "http://localhost:5791", ignoreHTTPSErrors: true },
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
	webServer: {
		command: "npx vite --port 5791 --strictPort",
		cwd: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-751",
		url: "http://localhost:5791",
		reuseExistingServer: false,
		timeout: 60_000,
	},
});
