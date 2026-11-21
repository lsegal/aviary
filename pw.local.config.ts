import base from "./playwright.config";
export default {
	...base,
	use: { ...base.use, baseURL: "http://localhost:5231" },
	webServer: {
		command: "pnpm exec vite --port 5231 --strictPort",
		url: "http://localhost:5231",
		reuseExistingServer: false,
		timeout: 60_000,
	},
};
