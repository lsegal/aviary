import base from "./playwright.config";
export default { ...base, use: { ...base.use, baseURL: "https://localhost:5191" }, webServer: undefined };
