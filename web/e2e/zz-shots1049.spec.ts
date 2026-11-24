import { test } from "@playwright/test";
import { mockMCP } from "./helpers/mockMCP";

const base = {
	currentVersion: "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	latestVersion: "17db712bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	upgradeAvailable: true,
	upgradeSupported: true,
	message: "origin/main is 1 commit(s) ahead of this checkout.",
	git: { dir: "/src/aviary", branch: "main", upstream: "origin/main", behind: 1, ahead: 0, dirty: false, supervised: true, hotReload: true },
};
const cases: [string, string, string | undefined][] = [
	["busy", "another git operation is running in the checkout (/src/aviary/.git/index.lock)", "git-busy"],
	["stale", "a stale git lock (/src/aviary/.git/index.lock, 42m0s old) is blocking updates", "git-stale-lock"],
];
for (const [name, error, code] of cases) {
	test(`shot ${name}`, async ({ page }) => {
		await page.setViewportSize({ width: 1280, height: 800 });
		await mockMCP(page, {
			agent_list: [], job_list: [], config_validate: [],
			server_version_check: { ...base, restart: { phase: "idle", jobs: [], held: false, autoUpdate: { version: base.latestVersion, error, code } } },
		});
		await page.goto("/system/overview");
		const card = page.locator('[data-testid="auto-update-error"]:visible').locator("xpath=..");
		await card.waitFor();
		await card.screenshot({ path: `/tmp/shot1049-${name}.png` });
	});
}
