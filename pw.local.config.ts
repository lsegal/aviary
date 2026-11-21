import base from "./playwright.config";
export default { ...base, use: { ...base.use, baseURL: "http://localhost:5674" }, webServer: { command: "pnpm exec vite --port 5674 --strictPort", url: "http://localhost:5674", reuseExistingServer: false, timeout: 60_000 } };
