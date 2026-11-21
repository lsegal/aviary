import base from "./playwright.config";
export default {
	...base,
	testDir: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-747/web/e2e",
	outputDir: "/tmp/pw747-results",
	use: { ...base.use, baseURL: "http://localhost:5947" },
	webServer: { command: "npx pnpm@10 exec vite --port 5947 --strictPort", cwd: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-747", url: "http://localhost:5947", reuseExistingServer: false, timeout: 120_000 },
};
