import base from "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-623/playwright.config.ts";
export default {
	...base,
	testDir: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-623/web/e2e",
	outputDir: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-623/test-results",
	use: { ...base.use, baseURL: "http://localhost:5287" },
	webServer: {
		command: "pnpm exec vite --port 5287 --strictPort",
		url: "http://localhost:5287",
		reuseExistingServer: true,
		timeout: 60_000,
		cwd: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-623",
	},
};
