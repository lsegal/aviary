import base from "/private/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-339/playwright.config.ts";
export default {
	...base,
	testDir: "/private/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-339/web/e2e",
	use: { ...base.use, baseURL: "http://localhost:52632" },
	webServer: { command: "pnpm exec vite --port 52632 --strictPort", url: "http://localhost:52632", reuseExistingServer: false, timeout: 60000 },
};
