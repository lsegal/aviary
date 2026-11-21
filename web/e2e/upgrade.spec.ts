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
	await page.goto("/system/overview");

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
	await page.goto("/system/overview");

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
	await page.goto("/system/overview");

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
	await page.goto("/system/overview");
	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	await expect(page.getByRole("dialog")).toContainText(
		"No jobs are running, so nothing will be stopped.",
	);
});

function gitCheck(dirty: boolean) {
	return {
		currentVersion: "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		latestVersion: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		upgradeAvailable: true,
		upgradeSupported: !dirty,
		message: dirty
			? "Updating is blocked: the checkout at /src/aviary has uncommitted changes."
			: "origin/main is 1 commit(s) ahead of this checkout.",
		git: {
			dir: "/src/aviary",
			branch: "main",
			upstream: "origin/main",
			behind: 1,
			ahead: 0,
			dirty,
			hotReload: true,
		},
		restart: { phase: "idle", jobs: [], held: false },
	};
}

test("dev checkout pulls from git without restarting", async ({ page }) => {
	const upgrades: Record<string, unknown>[] = [];
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: gitCheck(false),
		server_upgrade: (args?: Record<string, unknown>) => {
			upgrades.push(args ?? {});
			return {
				phase: "idle",
				jobs: [],
				held: false,
				noop: true,
				message:
					"Pulled 1 commit(s) (1111111 → abc1234). Hot reload is rebuilding Aviary with the new code.",
			};
		},
	});
	await page.goto("/system/overview");

	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	const dialog = page.getByRole("dialog");
	await expect(dialog).toContainText(
		"Update this checkout to origin/main (abc1234)",
	);
	await expect(dialog).toContainText("git pull --ff-only");
	await expect(dialog).toContainText("hot reload rebuilds Aviary");
	await expect(
		dialog.getByRole("button", { name: "When no jobs are running" }),
	).toHaveCount(0);

	await dialog.getByRole("button", { name: "Pull" }).click();
	await expect(dialog).toBeHidden();
	expect(upgrades).toEqual([
		{ version: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", when: "now" },
	]);
	await expect(page.getByTestId("update-notice").first()).toContainText(
		"Hot reload is rebuilding Aviary",
	);
	await expect(page.getByRole("status")).toHaveCount(0);
});

test("dev checkout with local changes blocks the update and says why", async ({
	page,
}) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: gitCheck(true),
	});
	await page.goto("/system/overview");

	await expect(
		page.getByRole("button", { name: "Upgrade", exact: true }),
	).toBeDisabled();
	await expect(page.getByTestId("version-message").first()).toContainText(
		"uncommitted changes",
	);
});

test("auto update says it waits for running jobs", async ({ page }) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: {
			...gitCheck(false),
			restart: {
				phase: "idle",
				jobs: runningJobs,
				held: false,
				autoUpdate: {
					version: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					state: "waiting-jobs",
					jobs: 2,
				},
			},
		},
	});
	await page.goto("/system/overview");

	await expect(page.getByTestId("auto-update-status").first()).toContainText(
		"Auto update is waiting for 2 running jobs to finish.",
	);
	await expect(page.getByTestId("auto-update-error")).toHaveCount(0);
});

test("auto update shows why its last attempt failed", async ({ page }) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: {
			...gitCheck(false),
			restart: {
				phase: "idle",
				jobs: [],
				held: false,
				autoUpdate: {
					version: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					error:
						"git pull --ff-only --quiet: exit status 1: fatal: unable to access remote",
				},
			},
		},
	});
	await page.goto("/system/overview");

	await expect(page.getByTestId("auto-update-error").first()).toContainText(
		"Auto update failed: git pull --ff-only --quiet: exit status 1",
	);
	await expect(page.getByTestId("auto-update-status")).toHaveCount(0);
});

test("restart button safely restarts and reports progress", async ({
	page,
}) => {
	const restarts: Record<string, unknown>[] = [];
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_restart: (args?: Record<string, unknown>) => {
			restarts.push(args ?? {});
			return { phase: "updating", when: "now", jobs: [], held: true };
		},
	});
	await page.goto("/system/overview");

	const restart = page.getByRole("button", { name: "Restart", exact: true });
	await expect(restart).toHaveAttribute("title", "Restart");
	await restart.click();
	expect(restarts).toEqual([{}]);
	await expect(page.getByRole("status")).toContainText("Restarting…");
	await expect(restart).toBeDisabled();
});

test("log out icon button returns to the login page", async ({ page }) => {
	await mockMCP(page, { agent_list: [], job_list: [], config_validate: [] });
	await page.goto("/system/overview");

	const logout = page.getByRole("button", { name: "Log out", exact: true });
	await expect(logout).toHaveAttribute("title", "Log out");
	await logout.click();
	await expect(page).toHaveURL(/\/login$/);
});

test("mobile system nav shows the restart and log out icon buttons", async ({
	page,
}) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await mockMCP(page, { agent_list: [], job_list: [], config_validate: [] });
	await page.goto("/system/overview");

	await expect(
		page.getByRole("button", { name: "Restart", exact: true }),
	).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Log out", exact: true }),
	).toBeVisible();
});

test("dev build shows its commit SHA as the version", async ({ page }) => {
	await page.routeWebSocket(/\/api\/ws/, (ws) => {
		ws.send(
			JSON.stringify({
				type: "health",
				ok: true,
				version: "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				goos: "darwin",
			}),
		);
	});
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: gitCheck(false),
	});
	await page.goto("/system/overview");

	await expect(
		page.getByText("Version 1111111 -> abc1234").last(),
	).toBeVisible();
	await expect(page.getByText("abc1234bbbb")).toHaveCount(0);
	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	await expect(page.getByRole("dialog")).toContainText(
		"Update this checkout to origin/main (abc1234)",
	);
});

test("dev build without a checkout says so instead of looking up to date", async ({
	page,
}) => {
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: {
			currentVersion: "dev",
			upgradeAvailable: false,
			upgradeSupported: false,
			noCheckout: true,
			message:
				"This dev build found no source checkout, so updates cannot be checked.",
			restart: { phase: "idle", jobs: [], held: false },
		},
	});
	await page.goto("/system/overview");

	const notice = page
		.getByTestId("update-no-checkout")
		.filter({ visible: true });
	await expect(notice).toContainText("Dev build — no checkout found");
	await expect(notice).toContainText("found no source checkout");
	await expect(
		page.getByRole("button", { name: "Upgrade", exact: true }),
	).toHaveCount(0);
});
