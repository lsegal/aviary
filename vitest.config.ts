import vue from "@vitejs/plugin-vue";
import { configDefaults, defineConfig } from "vitest/config";

// Unit tests for the web app (#1377). Playwright covers the app end to end
// under web/e2e; these test web/src modules directly and report their
// coverage to Qlty.
export default defineConfig({
	plugins: [vue()],
	test: {
		include: ["web/src/**/*.test.ts"],
		// Component tests run in a browser: vitest.component.config.ts.
		exclude: [...configDefaults.exclude, "web/src/**/*.component.test.ts"],
		coverage: {
			provider: "v8",
			// Repository-relative paths, so Qlty maps them without a fixup.
			reportsDirectory: "web/coverage",
			include: ["web/src/**/*.{ts,vue}"],
			exclude: [
				"web/src/**/*.test.ts",
				"web/src/**/*.d.ts",
				// Component-test harness and scenario helpers, run in the browser
				// by vitest.component.config.ts.
				"web/src/component-test/**",
				"web/src/scenarios/**",
			],
		},
	},
});
