import base from "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-340/playwright.config.ts";
export default { ...base, testDir: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-340/web/e2e", outputDir: "/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-340/test-results", use: { ...base.use, baseURL: "http://localhost:5662" }, webServer: undefined };
