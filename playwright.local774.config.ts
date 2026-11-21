import base from "./playwright.config";
export default {
	...base,
	use: { ...base.use, baseURL: "http://localhost:5774" },
	webServer: { ...base.webServer, command: "pnpm exec vite --port 5774 --strictPort", url: "http://localhost:5774", reuseExistingServer: false, timeout: 120000 },
};
