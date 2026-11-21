import base from "./playwright.config.ts";
export default { ...base, testDir: "/private/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-777/web/e2e", outputDir: "/tmp/pw777/results",
  use: { ...base.use, baseURL: "http://127.0.0.1:5491" },
  webServer: { command: "pnpm exec vite --host 127.0.0.1 --port 5491 --strictPort", cwd: "/private/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-777", url: "http://127.0.0.1:5491", reuseExistingServer: false, timeout: 60000 } };
