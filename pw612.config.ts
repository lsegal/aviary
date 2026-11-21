import base from "./playwright.config";
export default {
	...base,
	use: { ...base.use, baseURL: "http://localhost:5199" },
	webServer: { command: "pnpm exec vite --port 5199 --strictPort", url: "http://localhost:5199", reuseExistingServer: false, timeout: 60_000 },
};
