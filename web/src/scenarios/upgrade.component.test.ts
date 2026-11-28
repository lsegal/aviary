import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { mockMCP, routeWebSocket } from "../component-test/server";
import { openGearMenuItem } from "./helpers/sidebar";

/** The number of elements `loc` matches, as Playwright's toHaveCount. */
const count = (loc: { elements(): Element[] }) => () => loc.elements().length;

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

test("upgrade notice links the version to its release page", async () => {
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: versionCheck([]),
	});
	await renderApp("/system/overview");

	await expect
		.element(page.getByRole("link", { name: "v0.7.0" }))
		.toHaveAttribute(
			"href",
			"https://github.com/lsegal/aviary/releases/tag/v0.7.0",
		);
});

test("upgrade dialog lists the running jobs it will stop", async () => {
	const upgrades: Record<string, unknown>[] = [];
	mockMCP({
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
	await renderApp("/system/overview");

	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	const dialog = page.getByRole("dialog");
	await expect.element(dialog).toHaveTextContent("these 2 running jobs");
	await expect.element(dialog.getByText("Fix the parser")).toBeVisible();
	await expect.element(dialog.getByText("Add dark mode")).toBeVisible();
	await expect
		.element(dialog)
		.toHaveTextContent("Waiting jobs aren't touched.");

	await dialog.getByRole("button", { name: "Now" }).click();
	await expect.element(dialog).not.toBeInTheDocument();
	expect(upgrades).toEqual([{ version: "v0.7.0", when: "now" }]);
	await expect
		.element(page.getByRole("status"))
		.toHaveTextContent("Updating to v0.7.0");
});

test("upgrading when no jobs are running holds new jobs and can be canceled", async () => {
	const calls: string[] = [];
	mockMCP({
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
	await renderApp("/system/overview");

	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	await expect
		.element(page.getByRole("dialog"))
		.toHaveTextContent("this running job");
	await page.getByRole("button", { name: "When no jobs are running" }).click();

	const status = page.getByRole("status");
	await expect
		.element(status)
		.toHaveTextContent("Upgrade to v0.7.0 waits for 1 running job.");
	await expect.element(status).toHaveTextContent("New jobs are held");
	await expect
		.element(page.getByRole("button", { name: "Upgrade", exact: true }))
		.toBeDisabled();

	await status.getByRole("button", { name: "Cancel" }).click();
	await expect.element(status).not.toBeInTheDocument();
	expect(calls).toEqual(["upgrade:idle", "restart:cancel=true"]);
});

test("an upgrade waiting on in-flight MCP calls says so and can be canceled", async () => {
	const calls: string[] = [];
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: versionCheck([]),
		server_upgrade: () => {
			calls.push("upgrade");
			return {
				phase: "draining",
				when: "now",
				upgrade: true,
				version: "v0.7.0",
				mcpCalls: 2,
				jobs: [],
				held: true,
			};
		},
		server_restart: (args?: Record<string, unknown>) => {
			calls.push(`restart:cancel=${args?.cancel}`);
			return { phase: "idle", jobs: [], held: false };
		},
	});
	await renderApp("/system/overview");

	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	await page.getByRole("dialog").getByRole("button", { name: "Now" }).click();

	const status = page.getByRole("status");
	await expect
		.element(status)
		.toHaveTextContent("Upgrade to v0.7.0 pending: waiting on 2 MCP calls.");
	await status.getByRole("button", { name: "Cancel" }).click();
	await expect.element(status).not.toBeInTheDocument();
	expect(calls).toEqual(["upgrade", "restart:cancel=true"]);
});

test("upgrade dialog says nothing will stop when no jobs run", async () => {
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: versionCheck([]),
	});
	await renderApp("/system/overview");
	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	await expect
		.element(page.getByRole("dialog"))
		.toHaveTextContent("No jobs are running, so nothing will be stopped.");
});

function gitCheck(
	dirty: boolean,
	supervised = false,
	jobs: typeof runningJobs = [],
	hotReload = false,
) {
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
			supervised,
			hotReload,
		},
		restart: { phase: "idle", jobs, held: false },
	};
}

test("unsupervised dev checkout pulls from git and says to restart", async () => {
	const upgrades: Record<string, unknown>[] = [];
	mockMCP({
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
					"Pulled 1 commit(s) (1111111 → abc1234). Restart Aviary to run the new code.",
			};
		},
	});
	await renderApp("/system/overview");

	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	const dialog = page.getByRole("dialog");
	await expect
		.element(dialog)
		.toHaveTextContent("Update this checkout to origin/main (abc1234)");
	await expect.element(dialog).toHaveTextContent("git pull --ff-only");
	await expect.element(dialog).toHaveTextContent("restart Aviary afterwards");
	await expect
		.poll(
			count(dialog.getByRole("button", { name: "When no jobs are running" })),
		)
		.toBe(0);

	await dialog.getByRole("button", { name: "Pull" }).click();
	await expect.element(dialog).not.toBeInTheDocument();
	expect(upgrades).toEqual([
		{ version: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", when: "now" },
	]);
	await expect
		.element(page.getByTestId("update-notice").first())
		.toHaveTextContent("Restart Aviary to run the new code.");
	await expect.poll(count(page.getByRole("status"))).toBe(0);
});

test("supervised dev checkout pulls, then safely restarts onto the pulled code", async () => {
	const upgrades: Record<string, unknown>[] = [];
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: gitCheck(false, true, runningJobs.slice(0, 1)),
		server_upgrade: (args?: Record<string, unknown>) => {
			upgrades.push(args ?? {});
			return {
				phase: "waiting",
				when: "idle",
				version: "abc1234",
				jobs: runningJobs.slice(0, 1),
				held: true,
				message:
					"Pulled 1 commit(s) (1111111 → abc1234). Aviary restarts to build and run the new code.",
			};
		},
	});
	await renderApp("/system/overview");

	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	const dialog = page.getByRole("dialog");
	await expect
		.element(dialog)
		.toHaveTextContent(
			"Aviary then restarts and rebuilds from the pulled sources.",
		);
	await expect
		.element(dialog)
		.toHaveTextContent("resumes each one in its own session");
	await dialog
		.getByRole("button", { name: "When no jobs are running" })
		.click();

	await expect.element(dialog).not.toBeInTheDocument();
	expect(upgrades).toEqual([
		{ version: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", when: "idle" },
	]);
	await expect
		.element(page.getByRole("status"))
		.toHaveTextContent("waits for 1 running job");
	await expect
		.element(page.getByTestId("update-notice").first())
		.toHaveTextContent("Aviary restarts to build and run the new code.");
});

test("dev checkout under forgo hot-reloads the pulled code without restarting", async () => {
	const upgrades: Record<string, unknown>[] = [];
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: gitCheck(false, true, runningJobs.slice(0, 1), true),
		server_upgrade: (args?: Record<string, unknown>) => {
			upgrades.push(args ?? {});
			return {
				phase: "idle",
				jobs: runningJobs.slice(0, 1),
				held: false,
				noop: true,
				message:
					"Pulled 1 commit(s) (1111111 → abc1234). The backend hot-reloads the new code, and restarts only if forgo cannot apply it.",
			};
		},
	});
	await renderApp("/system/overview");

	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	const dialog = page.getByRole("dialog");
	await expect
		.element(dialog)
		.toHaveTextContent(
			"hot-reloads the pulled code in place, so running jobs keep running",
		);
	await expect
		.element(dialog)
		.toHaveTextContent(
			"If a change can't be hot-reloaded, the restart stops this running job",
		);
	await dialog.getByRole("button", { name: "Now" }).click();

	await expect.element(dialog).not.toBeInTheDocument();
	expect(upgrades).toEqual([
		{ version: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", when: "now" },
	]);
	await expect
		.element(page.getByTestId("update-notice").first())
		.toHaveTextContent("The backend hot-reloads the new code");
	await expect.poll(count(page.getByRole("status"))).toBe(0);
});

test("dev checkout with local changes blocks the update and says why", async () => {
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: gitCheck(true),
	});
	await renderApp("/system/overview");

	await expect
		.element(page.getByRole("button", { name: "Upgrade", exact: true }))
		.toBeDisabled();
	await expect
		.element(page.getByTestId("version-message").first())
		.toHaveTextContent("uncommitted changes");
});

test("auto update applies with jobs running", async () => {
	mockMCP({
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
					state: "applying",
				},
			},
		},
	});
	await renderApp("/system/overview");

	await expect
		.element(page.getByTestId("auto-update-status").first())
		.toHaveTextContent("Auto update is pulling the checkout…");
	await expect
		.element(page.getByTestId("auto-update-status").first())
		.not.toHaveTextContent("running job");
	await expect.poll(count(page.getByTestId("auto-update-error"))).toBe(0);
});

test("auto update shows why its last attempt failed", async () => {
	mockMCP({
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
	await renderApp("/system/overview");

	await expect
		.element(page.getByTestId("auto-update-error").first())
		.toHaveTextContent(
			"Auto update failed: git pull --ff-only --quiet: exit status 1",
		);
	await expect.poll(count(page.getByTestId("auto-update-status"))).toBe(0);
});

function autoUpdateFailure(error: string, code?: string) {
	return {
		...gitCheck(false),
		restart: {
			phase: "idle",
			jobs: [],
			held: false,
			autoUpdate: {
				version: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				error,
				code,
			},
		},
	};
}

function pulled(upgrades: Record<string, unknown>[]) {
	return (args?: Record<string, unknown>) => {
		upgrades.push(args ?? {});
		return {
			phase: "idle",
			jobs: [],
			held: false,
			noop: true,
			message:
				"Pulled 1 commit(s) (1111111 → abc1234). No backend code changed.",
		};
	};
}

test("auto update deferred by a busy checkout says so and retries", async () => {
	const upgrades: Record<string, unknown>[] = [];
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: autoUpdateFailure(
			"another git operation is running in the checkout (/src/aviary/.git/index.lock)",
			"git-busy",
		),
		server_upgrade: pulled(upgrades),
	});
	await renderApp("/system/overview");

	const notice = page.locator('[data-testid="auto-update-error"]:visible');
	await expect
		.element(notice)
		.toHaveTextContent(
			"Update deferred: another git operation is running, retrying.",
		);
	await expect.element(notice).not.toHaveTextContent("Auto update failed");
	await notice.getByRole("button", { name: "Retry" }).click();
	await expect
		.poll(() => upgrades)
		.toEqual([
			{ version: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", when: "now" },
		]);
});

test("auto update blocked by a stale index.lock offers to remove it", async () => {
	const upgrades: Record<string, unknown>[] = [];
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: autoUpdateFailure(
			"a stale git lock (/src/aviary/.git/index.lock, 42m0s old) is blocking updates",
			"git-stale-lock",
		),
		server_upgrade: pulled(upgrades),
	});
	await renderApp("/system/overview");

	const notice = page.locator('[data-testid="auto-update-error"]:visible');
	await expect
		.element(notice)
		.toHaveTextContent(
			"Update blocked: a stale git lock (/src/aviary/.git/index.lock, 42m0s old) is blocking updates.",
		);
	await notice
		.getByRole("button", { name: "Remove stale lock and update" })
		.click();
	await expect
		.poll(() => upgrades)
		.toEqual([
			{
				version: "abc1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				when: "now",
				remove_stale_lock: true,
			},
		]);
});

test("the gear menu's Restart safely restarts and reports progress", async () => {
	const restarts: Record<string, unknown>[] = [];
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_restart: (args?: Record<string, unknown>) => {
			restarts.push(args ?? {});
			return { phase: "updating", when: "now", jobs: [], held: true };
		},
	});
	await renderApp("/system/overview");

	await openGearMenuItem("Restart");
	await expect.poll(() => restarts).toEqual([{}]);
	await expect
		.element(page.getByRole("status"))
		.toHaveTextContent("Restarting…");
	await page
		.getByTestId("sidebar-footer")
		.getByTestId("gear-menu-button")
		.click();
	await expect
		.element(
			page.getByTestId("gear-menu").getByRole("menuitem", { name: "Restart" }),
		)
		.toHaveAttribute("data-disabled", "");
});

test("the gear menu's Logout returns to the login page", async () => {
	mockMCP({ agent_list: [], job_list: [], config_validate: [] });
	await renderApp("/system/overview");

	await openGearMenuItem("Logout");
	await expect.poll(currentURL).toMatch(/\/login$/);
});

test("the mobile header has the gear menu instead of restart and log out buttons", async () => {
	await page.viewport(390, 844);
	mockMCP({ agent_list: [], job_list: [], config_validate: [] });
	await renderApp("/system/overview");

	await expect
		.poll(count(page.getByRole("button", { name: "Restart", exact: true })))
		.toBe(0);
	await expect
		.poll(count(page.getByRole("button", { name: "Log out", exact: true })))
		.toBe(0);
	await page
		.getByTestId("mobile-header-status")
		.getByTestId("gear-menu-button")
		.click();
	await page
		.getByTestId("gear-menu")
		.getByRole("menuitem", { name: "Settings" })
		.click();
	await expect.poll(currentURL).toBe("/system/settings");
});

test("dev build shows its commit SHA as the version", async () => {
	routeWebSocket(/\/api\/ws/, (ws) => {
		ws.send(
			JSON.stringify({
				type: "health",
				ok: true,
				version: "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				goos: "darwin",
			}),
		);
	});
	mockMCP({
		agent_list: [],
		job_list: [],
		config_validate: [],
		server_version_check: gitCheck(false),
	});
	await renderApp("/system/overview");

	const footer = page.getByTestId("sidebar-footer");
	await expect
		.element(footer.getByTestId("server-version"))
		.toHaveText("1111111");
	await expect
		.element(footer.getByTestId("update-icon"))
		.toHaveAttribute("aria-label", "Update available: abc1234");
	await expect.poll(count(page.getByText("abc1234bbbb"))).toBe(0);
	await page.getByRole("button", { name: "Upgrade", exact: true }).click();
	await expect
		.element(page.getByRole("dialog"))
		.toHaveTextContent("Update this checkout to origin/main (abc1234)");
});

test("dev build without a checkout says so instead of looking up to date", async () => {
	mockMCP({
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
	await renderApp("/system/overview");

	const notice = page.locator('[data-testid="update-no-checkout"]:visible');
	await expect
		.element(notice)
		.toHaveTextContent("Dev build — no checkout found");
	await expect.element(notice).toHaveTextContent("found no source checkout");
	await expect
		.poll(count(page.getByRole("button", { name: "Upgrade", exact: true })))
		.toBe(0);
});
