import base from "./playwright.config.ts";
export default { ...base, testDir: "./web/e2e", use: { ...base.use, baseURL: "http://localhost:5199" }, webServer: undefined };
