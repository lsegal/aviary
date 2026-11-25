// E2E specs import from here instead of @playwright/test so every test's page
// records JavaScript coverage when AVIARY_E2E_COVERAGE is set (#1308).
import { test as base } from "@playwright/test";
import { coverageEnabled, coverageReport } from "./coverage";

export * from "@playwright/test";

export const test = base.extend<{ jsCoverage: undefined }>({
	jsCoverage: [
		async ({ page, browserName }, use) => {
			const enabled = coverageEnabled && browserName === "chromium";
			if (enabled) {
				await page.coverage.startJSCoverage({ resetOnNavigation: false });
			}
			await use(undefined);
			if (enabled && !page.isClosed()) {
				await coverageReport().add(await page.coverage.stopJSCoverage());
			}
		},
		{ auto: true },
	],
});
