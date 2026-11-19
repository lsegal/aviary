import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

const STATUS = {
	ready_label: "ready",
	projects: [{ name: "lsegal/aviary", running: 0, waiting: 0, jobs: 0 }],
};

// Shapes follow internal/config/projects.go and internal/projects/api.go.
const CONFIG = {
	server: { port: 16677 },
	agents: [{ name: "atlas", model: "anthropic/claude-sonnet-4-5" }],
	projects: {
		list: [{ target: "lsegal/aviary", agents: ["claude/opus"] }],
		allowed_commenters: ["lsegal", "teammate"],
		webhook: { path: "/hooks/gh" },
	},
};

const CLIS = {
	clis: [
		{
			name: "claude",
			installed: true,
			auth: "signed in",
			quota: "38% of 5h quota left",
			models: ["claude/opus", "claude/sonnet"],
			status: "ok",
		},
		{
			name: "codex",
			installed: true,
			auth: "signed in",
			quota: "quota ok",
			models: ["codex/gpt-5.5", "codex/gpt-5.5-mini"],
			status: "ok",
		},
		{ name: "gemini", installed: true, auth: "signed out", status: "warn" },
	],
};

const READY_CHECK = {
	target: "lsegal/zvidlib",
	kind: "repo",
	fork: true,
	ok: true,
	checks: [
		{
			name: "github",
			status: "ok",
			message: "gh is signed in and can read lsegal/zvidlib",
		},
		{
			name: "fork",
			status: "warn",
			message:
				"You can't push to lsegal/zvidlib, so pull requests will come from your fork",
		},
		{
			name: "ready_label",
			status: "ok",
			message: '"ready" exists, with 4 open issues',
		},
		{ name: "clis", status: "ok", message: "Found claude and codex" },
		{
			name: "skills",
			status: "ok",
			message: "gh-fix 1.6.0 installed for all 2",
		},
	],
};

type Args = Record<string, unknown>;
type Calls = Record<string, Args[]>;

async function setup(
	page: Page,
	opts: {
		check?: unknown;
		config?: unknown;
		cliList?: () => Promise<unknown>;
	} = {},
) {
	await setAuthToken(page);
	const calls: Calls = {};
	const record = (tool: string, args?: Args) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	await mockMCP(page, {
		agent_list: [],
		project_status: STATUS,
		project_job_list: { jobs: [], total: 0 },
		config_get: opts.config ?? CONFIG,
		config_save: (args?: Args) => {
			record("config_save", args);
			return "saved";
		},
		project_check: (args?: Args) => {
			record("project_check", args);
			return opts.check ?? READY_CHECK;
		},
		project_add: (args?: Args) => {
			record("project_add", args);
			return { ok: true };
		},
		project_cli_list: async () => (opts.cliList ? opts.cliList() : CLIS),
		project_clone_list: {
			roots: ["/tmp"],
			clones: [
				{
					path: "/tmp/aviary-209",
					repository: "lsegal/aviary",
					issue: 209,
					inUse: false,
				},
				{
					path: "/tmp/aviary-218",
					repository: "lsegal/aviary",
					issue: 218,
					pullRequest: 233,
					inUse: true,
					reason: "job is running",
				},
			],
		},
		project_clone_clear: (args?: Args) => {
			record("project_clone_clear", args);
			return {
				removed: [{ path: "/tmp/aviary-209", size: 157286400 }],
				kept: 1,
				reclaimed: 157286400,
			};
		},
	});
	return calls;
}

function savedSection(calls: Calls) {
	return JSON.parse(String(calls.config_save?.at(-1)?.config)).projects;
}

test("adding a project from the single field starts a watch without writing aviary.yaml", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/projects/lsegal%2Faviary");
	await page.getByTestId("sidebar-add-project").click();

	const dialog = page.getByRole("dialog", { name: "Watch a repository" });
	await expect(dialog).toBeVisible();
	await dialog
		.getByLabel("Repository or project board")
		.fill("https://github.com/lsegal/zvidlib");

	const checks = dialog.getByTestId("add-project-checks");
	await expect(checks).toContainText("Ready to go");
	await expect(checks).toContainText(
		"gh is signed in and can read lsegal/zvidlib",
	);
	await expect(checks).toContainText("pull requests will come from your fork");
	await expect(checks.locator('[data-check="fork"]')).toHaveAttribute(
		"data-status",
		"warn",
	);
	await expect(dialog).toContainText("Up to 3 jobs at once");
	await expect(dialog).toContainText("Runs on codex");
	await expect(dialog.locator("footer code")).toHaveText(
		"aviary watch lsegal/zvidlib",
	);

	await dialog.getByRole("button", { name: "Start watching" }).click();
	await expect(page).toHaveURL("/projects/lsegal%2Fzvidlib");
	await expect(dialog).toBeHidden();
	expect(calls.project_add).toEqual([{ target: "lsegal/zvidlib" }]);
	expect(calls.project_check?.at(-1)).toEqual({
		target: "https://github.com/lsegal/zvidlib",
	});
	expect(calls.config_save).toBeUndefined();
});

test("add-project checks report each failing prerequisite", async ({
	page,
}) => {
	const calls = await setup(page, {
		check: {
			target: "lsegal/zvidlib",
			kind: "repo",
			ok: false,
			checks: [
				{
					name: "github",
					status: "ok",
					message: "gh is signed in and can read lsegal/zvidlib",
				},
				{
					name: "ready_label",
					status: "fail",
					message:
						'lsegal/zvidlib has no "ready" label; create it or pick another ready label',
				},
				{
					name: "clis",
					status: "fail",
					message:
						"No coding CLI found; install one of claude, codex, gemini, opencode or cline",
				},
				{
					name: "skills",
					status: "fail",
					message: "gh-fix cannot be installed until a coding CLI is found",
				},
			],
		},
	});
	await page.goto("/projects/lsegal%2Faviary?add=1");
	const dialog = page.getByRole("dialog");
	await dialog.getByLabel("Repository or project board").fill("lsegal/zvidlib");

	const checks = dialog.getByTestId("add-project-checks");
	await expect(checks).toContainText("Needs attention");
	await expect(checks.locator('[data-status="fail"]')).toHaveCount(3);
	for (const name of ["ready_label", "clis", "skills"]) {
		await expect(checks.locator(`[data-check="${name}"]`)).toHaveAttribute(
			"data-status",
			"fail",
		);
	}
	await expect(checks).toContainText('has no "ready" label');
	await expect(
		checks.getByRole("link", { name: "Manage skills" }),
	).toBeVisible();
	await expect(
		dialog.getByRole("button", { name: "Start anyway" }),
	).toBeEnabled();

	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect(dialog).toBeHidden();
	await expect(page).toHaveURL("/projects/lsegal%2Faviary");
	expect(calls.project_add).toBeUndefined();
});

test("changing a setting writes only the differing values", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/projects/lsegal%2Faviary");
	await page
		.getByRole("main")
		.getByRole("link", { name: "Settings", exact: true })
		.click();
	await expect(page).toHaveURL("/projects/lsegal%2Faviary/settings");

	const yaml = page.getByTestId("project-yaml");
	await expect(yaml).toHaveText(
		[
			"projects:",
			"  list:",
			"    - target: lsegal/aviary",
			"      agents: [claude/opus]",
			"  allowed_commenters: [lsegal, teammate]",
			"  webhook:",
			"    path: /hooks/gh",
		].join("\n"),
	);
	const jobsLabel = page.locator("label", {
		has: page.locator('[data-field="concurrency"]'),
	});
	await expect(jobsLabel).toContainText("default");
	await expect(page.locator('[data-field="allowed_commenters"]')).toHaveValue(
		"lsegal, teammate",
	);
	await expect(
		page.getByRole("button", { name: "Save", exact: true }),
	).toBeDisabled();

	await page.locator('[data-field="concurrency"]').fill("6");
	await expect(jobsLabel).not.toContainText("default");
	await page
		.getByRole("switch", { name: "Merge pull requests when CI passes" })
		.click();
	await page.locator('[data-field="delivery"]').selectOption("webhook");
	await expect(yaml).toContainText("concurrency: 6");
	await expect(yaml).toContainText("no_merge: true");
	await expect(yaml).toContainText("delivery: webhook");

	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect(page.getByTestId("save-note")).toHaveText(
		"Saved and applied to the running watch",
	);
	expect(savedSection(calls)).toEqual({
		list: [{ target: "lsegal/aviary", agents: ["claude/opus"] }],
		allowed_commenters: ["lsegal", "teammate"],
		webhook: { path: "/hooks/gh" },
		concurrency: 6,
		no_merge: true,
		delivery: "webhook",
	});
	// The rest of the config goes back unchanged.
	const saved = JSON.parse(String(calls.config_save?.at(-1)?.config));
	expect(saved.agents[0].name).toBe("atlas");

	// Back to defaults, the values drop out again.
	await page.locator('[data-field="concurrency"]').fill("3");
	await page
		.getByRole("switch", { name: "Merge pull requests when CI passes" })
		.click();
	await page.locator('[data-field="delivery"]').selectOption("poll");
	await expect(yaml).not.toContainText("concurrency:");
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect.poll(() => calls.config_save?.length).toBe(2);
	expect(savedSection(calls)).toEqual(CONFIG.projects);
});

test("discard restores the saved settings", async ({ page }) => {
	await setup(page);
	await page.goto("/projects/lsegal%2Faviary/settings");
	await page.locator('[data-field="ready_label"]').fill("agent-ready");
	await expect(page.getByTestId("project-yaml")).toContainText(
		"ready_label: agent-ready",
	);
	await page.getByRole("button", { name: "Discard" }).click();
	await expect(page.locator('[data-field="ready_label"]')).toHaveValue("ready");
	await expect(page.getByTestId("project-yaml")).not.toContainText(
		"ready_label",
	);
});

test("CLI probe shows its cached result and refreshes in the background", async ({
	page,
}) => {
	let release: () => void = () => {};
	const gate = new Promise<void>((r) => {
		release = r;
	});
	await page.addInitScript(
		(clis) =>
			localStorage.setItem("aviary.projects.cli-probe", JSON.stringify(clis)),
		{
			clis: [
				{
					name: "claude",
					auth: "signed in",
					quota: "cached quota",
					models: ["claude/opus", "claude/sonnet"],
				},
			],
		},
	);
	const calls = await setup(page, {
		cliList: async () => {
			await gate;
			return CLIS;
		},
	});
	await page.goto("/projects/lsegal%2Faviary/settings");

	const claude = page.locator('[data-cli="claude"]');
	await expect(claude).toContainText("cached quota");
	await expect(page.getByTestId("cli-refreshing")).toBeVisible();
	release();
	await expect(page.getByTestId("cli-refreshing")).toBeHidden();
	await expect(claude).toContainText("38% of 5h quota left");
	await expect(page.locator('[data-cli="gemini"]')).toContainText("signed out");

	const yaml = page.getByTestId("project-yaml");
	const codex = page.locator('[data-cli="codex"]');
	await codex.getByRole("button", { name: "Use" }).click();
	await codex.getByRole("button", { name: "gpt-5.5", exact: true }).click();
	await expect(yaml).toContainText("agents: [claude/opus, codex/gpt-5.5]");
	await page.getByRole("button", { name: "Move codex up" }).click();
	await expect(yaml).toContainText("agents: [codex/gpt-5.5, claude/opus]");
	await page
		.locator('[aria-label="Add an Aviary agent"]')
		.selectOption("aviary/atlas");
	await expect(yaml).toContainText(
		"agents: [codex/gpt-5.5, claude/opus, aviary/atlas]",
	);
	expect(calls.config_save).toBeUndefined();
});

test("a project without its own CLI order uses the shared one", async ({
	page,
}) => {
	await setup(page, {
		config: { server: {}, projects: { agents: ["claude/sonnet"] } },
	});
	await page.goto("/projects/lsegal%2Faviary/settings");
	await expect(page.getByTestId("cli-shared")).toBeVisible();
	await expect(page.locator('[data-cli="claude"]')).toContainText("1");
	await expect(page.getByTestId("project-yaml")).toHaveText(
		"projects:\n  agents: [claude/sonnet]",
	);
});

test("clones show what's in use and unused ones can be cleared", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/projects/lsegal%2Faviary/settings");
	const rows = page.getByTestId("clone-row");
	await expect(rows).toHaveCount(2);
	await expect(rows.first()).toContainText("lsegal/aviary#209");
	await expect(rows.first()).toContainText("unused");
	await expect(rows.nth(1)).toContainText("PR #233");
	await expect(rows.nth(1)).toContainText("in use");

	await page.getByRole("button", { name: "Clear unused clones" }).click();
	await expect(page.getByTestId("clone-message")).toHaveText(
		"Removed 1 clone, reclaimed 150 MB, kept 1 in use.",
	);
	expect(calls.project_clone_clear).toEqual([{}]);
});

test("change settings first saves nothing for defaults, then starts the watch", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/projects?add=1");
	const dialog = page.getByRole("dialog");
	await dialog.getByLabel("Repository or project board").fill("lsegal/zvidlib");
	await expect(dialog.getByTestId("add-project-checks")).toContainText(
		"Ready to go",
	);
	await dialog.getByRole("button", { name: "Change settings first" }).click();
	await expect(page).toHaveURL("/projects/lsegal%2Fzvidlib/settings?add=1");
	await expect(page.getByTestId("project-yaml")).not.toContainText("zvidlib");

	await page.getByRole("button", { name: "Save and start watching" }).click();
	await expect(page).toHaveURL("/projects/lsegal%2Fzvidlib");
	expect(calls.config_save).toBeUndefined();
	expect(calls.project_add).toEqual([{ target: "lsegal/zvidlib" }]);
});

test("a new project with its own settings is written before it starts", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/projects/lsegal%2Fzvidlib/settings?add=1");
	const codex = page.locator('[data-cli="codex"]');
	await codex.getByRole("button", { name: "gpt-5.5", exact: true }).click();
	await page.getByRole("button", { name: "Save and start watching" }).click();
	await expect(page).toHaveURL("/projects/lsegal%2Fzvidlib");
	expect(savedSection(calls).list).toEqual([
		CONFIG.projects.list[0],
		{ target: "lsegal/zvidlib", agents: ["codex/gpt-5.5"] },
	]);
	expect(calls.project_add).toEqual([{ target: "lsegal/zvidlib" }]);
});
