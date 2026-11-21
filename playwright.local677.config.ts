import base from "./playwright.config";
export default { ...base, use: { ...base.use, baseURL: "http://localhost:5199" }, webServer: undefined };
