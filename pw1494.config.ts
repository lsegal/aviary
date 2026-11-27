import base from "/private/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-1490/playwright.config";
export default { ...base, testDir: "/private/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-1490/web/e2e", outputDir: "/tmp/pw1494-results", use: { ...base.use, baseURL: "https://localhost:5391" }, webServer: undefined };
