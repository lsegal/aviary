import base from "./playwright.config";

export default {
	...base,
	use: { ...base.use, baseURL: "http://127.0.0.1:5761" },
	webServer: {
		command: "npx -y pnpm@10 exec vite --host 127.0.0.1 --port 5761 --strictPort",
		url: "http://127.0.0.1:5761",
		reuseExistingServer: false,
		timeout: 60_000,
	},
};
