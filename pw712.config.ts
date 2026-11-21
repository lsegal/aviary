import base from "./playwright.config";
export default { ...base, webServer: undefined, use: { ...base.use, baseURL: "http://localhost:54167" } };
