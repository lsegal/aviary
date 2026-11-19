import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

const runningJobs = [
	{ source: "projects", id: "lsegal/aviary#12", title: "Fix the parser" },
	{ source: "projects", id: "lsegal/aviary#14", title: "Add dark mode" },
];

function versionCheck(jobs: typeof runningJobs) {
	return {
		currentVersion: "v0.6.2",
		latestVersion: "v0.7.0",
		upgradeAvailable: true,
		upgradeSupported: true,
		releaseUrl: "https://github.com/lsegal/aviary/releases/tag/v0.7.0",
		restart: { phase: "idle", jobs, held: false },
	};
}

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
});

test("upgrade notice links the version to its release page", async ({
	page,
}) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: versionCheck([]),
	});
	await page.goto("/overview");

	await expect(page.getByRole("link", { name: "v0.7.0" })).toHaveAttribute(
		"href",
		"https://github.com/lsegal/aviary/releases/tag/v0.7.0",
	);
});

test("upgrade dialog lists the running jobs it will stop", async ({ page }) => {
	const upgrades: Record<string, unknown>[] = [];
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: versionCheck(runningJobs),
		server_upgrade: (args?: Record<string, unknown>) => {
			upgrades.push(args ?? {});
			return {
				phase: "updating",
				when: "now",
				upgrade: true,
				version: "v0.7.0",
				jobs: runningJobs,
				held: true,
			};
		},
	});
	await page.goto("/overview");

	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	const dialog = page.getByRole("dialog");
	await expect(dialog).toContainText("these 2 running jobs");
	await expect(dialog.getByText("Fix the parser")).toBeVisible();
	await expect(dialog.getByText("Add dark mode")).toBeVisible();
	await expect(dialog).toContainText("Waiting jobs aren't touched.");

	await dialog.getByRole("button", { name: "Now" }).click();
	await expect(dialog).toBeHidden();
	expect(upgrades).toEqual([{ version: "v0.7.0", when: "now" }]);
	await expect(page.getByRole("status")).toContainText("Updating to v0.7.0");
});

test("upgrading when no jobs are running holds new jobs and can be canceled", async ({
	page,
}) => {
	const calls: string[] = [];
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: versionCheck(runningJobs.slice(0, 1)),
		server_upgrade: (args?: Record<string, unknown>) => {
			calls.push(`upgrade:${args?.when}`);
			return {
				phase: "waiting",
				when: "idle",
				upgrade: true,
				version: "v0.7.0",
				jobs: runningJobs.slice(0, 1),
				held: true,
			};
		},
		server_restart: (args?: Record<string, unknown>) => {
			calls.push(`restart:cancel=${args?.cancel}`);
			return { phase: "idle", jobs: runningJobs.slice(0, 1), held: false };
		},
	});
	await page.goto("/overview");

	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	await expect(page.getByRole("dialog")).toContainText("this running job");
	await page.getByRole("button", { name: "When no jobs are running" }).click();

	const status = page.getByRole("status");
	await expect(status).toContainText(
		"Upgrade to v0.7.0 waits for 1 running job.",
	);
	await expect(status).toContainText("New jobs are held");
	await expect(
		page.getByRole("button", { name: "Upgrade", exact: true }),
	).toBeDisabled();

	await status.getByRole("button", { name: "Cancel" }).click();
	await expect(status).toBeHidden();
	expect(calls).toEqual(["upgrade:idle", "restart:cancel=true"]);
});

test("upgrade dialog says nothing will stop when no jobs run", async ({
	page,
}) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: versionCheck([]),
	});
	await page.goto("/overview");
	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	await expect(page.getByRole("dialog")).toContainText(
		"No jobs are running, so nothing will be stopped.",
	);
});
