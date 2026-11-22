import base from "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-923/playwright.config";
import { defineConfig } from "@playwright/test";
export default defineConfig({
	...base,
	testDir: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-923/web/e2e",
	outputDir: "/tmp/pw923/results",
	use: { ...base.use, baseURL: "http://127.0.0.1:5923" },
	webServer: { command: "pnpm exec vite --host 127.0.0.1 --port 5923 --strictPort", cwd: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-923", url: "http://127.0.0.1:5923", reuseExistingServer: false, timeout: 60_000 },
});
