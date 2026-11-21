import base from "./playwright.config";
export default {
	...base,
	testDir: "./web/e2e",
	use: { ...base.use, baseURL: "http://localhost:5703" },
	webServer: undefined,
};
