import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vitest/config";

// Unit tests for the web app (#1377). Playwright covers the app end to end
// under web/e2e; these test web/src modules directly and report their
// coverage to Qlty.
export default defineConfig({
	plugins: [vue()],
	test: {
		include: ["web/src/**/*.test.ts"],
		coverage: {
			provider: "v8",
			// Repository-relative paths, so Qlty maps them without a fixup.
			reportsDirectory: "web/coverage",
			include: ["web/src/**/*.{ts,vue}"],
			exclude: ["web/src/**/*.test.ts", "web/src/**/*.d.ts"],
		},
	},
});
