import base from "./playwright.config";
export default { ...base, use: { ...base.use, baseURL: "http://localhost:5287" }, webServer: { ...base.webServer, url: "http://localhost:5287", command: "true", reuseExistingServer: true } };
