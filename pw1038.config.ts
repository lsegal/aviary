import base from "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-1038/playwright.config";
export default {
	...base,
	testDir: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-1038/web/e2e",
	use: { ...base.use, baseURL: "https://127.0.0.1:5937", ignoreHTTPSErrors: true },
	webServer: { command: "pnpm exec vite --host 127.0.0.1 --port 5937 --strictPort", url: "https://127.0.0.1:5937", reuseExistingServer: false, cwd: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-1038", ignoreHTTPSErrors: true, timeout: 120000 },
};
