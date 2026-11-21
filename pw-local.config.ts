import base from "./playwright.config";
export default {
	...base,
	use: { ...base.use, baseURL: "http://localhost:5191" },
	webServer: {
		command: "npx vite --port 5191 --strictPort",
		url: "http://localhost:5191",
		reuseExistingServer: false,
		timeout: 60_000,
	},
};
