import base from "./playwright.config";
export default {
	...base,
	use: { ...base.use, baseURL: "http://localhost:15873" },
	webServer: { command: "AVIARY_DEV_NO_TLS=1 node_modules/.bin/vite --port 15873 --strictPort", url: "http://localhost:15873", ignoreHTTPSErrors: true, reuseExistingServer: false, timeout: 60_000 },
};
