import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

const STATUS = {
	ready_label: "ready",
	projects: [{ name: "lsegal/aviary", running: 0, waiting: 0, jobs: 0 }],
};

// Shapes follow internal/config/projects.go and internal/projects/api.go.
const CONFIG = {
	server: { port: 16677 },
	agents: [
		{ name: "atlas", model: "anthropic/claude-sonnet-4-5" },
		{
			name: "coder",
			model: "cli/claude/opus",
			fallbacks: ["cli/codex/gpt-5.5"],
		},
	],
	projects: {
		list: [{ target: "lsegal/aviary", agent: "coder" }],
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

const CLONES = {
	roots: ["/tmp"],
	clones: [
		{
			path: "/tmp/aviary-209",
			repository: "lsegal/aviary",
			issue: 209,
			inUse: false,
			reason: "not owned by this instance: no job here records it",
			size: 157286400,
		},
		{
			path: "/tmp/aviary-218",
			repository: "lsegal/aviary",
			issue: 218,
			pullRequest: 233,
			inUse: true,
			reason: "job is paused",
		},
		{
			path: "/elsewhere/aviary-230",
			repository: "lsegal/aviary",
			issue: 230,
			inUse: false,
			kept: true,
			reason: "outside the configured temp directories; remove it by hand",
			size: 0,
		},
	],
	totalSize: 157286400,
	unmeasured: 1,
};

async function setup(
	page: Page,
	opts: {
		check?: unknown;
		config?: unknown;
		jobs?: { number: number; status: string }[];
		clonesError?: string;
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
		project_job_list: (args?: Args) => {
			record("project_job_list", args);
			return { jobs: opts.jobs ?? [], total: opts.jobs?.length ?? 0 };
		},
		project_remove: (args?: Args) => {
			record("project_remove", args);
			return {
				target: args?.target,
				removed: true,
				stopped_jobs: (opts.jobs ?? []).map((j) => j.number),
			};
		},
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
		provider_cli_list: async () => CLIS,
		project_clone_list: () => {
			if (opts.clonesError) throw new Error(opts.clonesError);
			return CLONES;
		},
		project_clone_clear: (args?: Args) => {
			record("project_clone_clear", args);
			return {
				removed: [{ path: "/tmp/aviary-209", size: 157286400 }],
				kept: 2,
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
	// lsegal/zvidlib is attached to no agent, and models.defaults has none.
	await expect(dialog).toContainText("Runs on no agent");
	await expect(dialog.locator("footer code")).toHaveText(
		"aviary watch lsegal/zvidlib",
	);

	await dialog.getByRole("button", { name: "Start watching" }).click();
	await expect(page).toHaveURL("/projects/lsegal%2Fzvidlib");
	await expect(dialog).toBeHidden();
	expect(calls.project_add).toEqual([
		{ target: "lsegal/zvidlib", runtime: true },
	]);
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
						"No CLI provider found; install one of claude, codex, gemini, opencode or cline",
				},
				{
					name: "skills",
					status: "fail",
					message: "gh-fix cannot be installed until a CLI provider is found",
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
			"      agent: coder",
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
	// Browser delivery is the default and the only mode with browser switches.
	await expect(page.locator('[data-field="delivery"]')).toHaveValue("browser");
	await expect(page.locator('[data-field="interval"]')).toHaveAttribute(
		"placeholder",
		"20s",
	);
	await page
		.getByRole("switch", {
			name: "Read pages it no longer recognizes from capped screenshots",
		})
		.click();
	await expect(yaml).toContainText("browser_vision: true");
	await page.locator('[data-field="delivery"]').selectOption("webhook");
	await expect(
		page.getByRole("switch", {
			name: "Show the browser it reads GitHub with, for debugging",
		}),
	).toHaveCount(0);
	await expect(page.locator('[data-field="interval"]')).toHaveAttribute(
		"placeholder",
		"30s",
	);
	await expect(yaml).not.toContainText("browser_vision");
	await expect(yaml).toContainText("concurrency: 6");
	await expect(yaml).toContainText("no_merge: true");
	await expect(yaml).toContainText("delivery: webhook");

	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect(page.getByTestId("save-note")).toHaveText(
		"Saved and applied to the running watch",
	);
	expect(savedSection(calls)).toEqual({
		list: [{ target: "lsegal/aviary", agent: "coder" }],
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
	await page.locator('[data-field="delivery"]').selectOption("browser");
	await page
		.getByRole("switch", {
			name: "Read pages it no longer recognizes from capped screenshots",
		})
		.click();
	await expect(yaml).not.toContainText("concurrency:");
	await expect(yaml).not.toContainText("delivery:");
	await expect(yaml).not.toContainText("browser_vision");
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

test("a project runs its jobs on its agent's model and fallbacks", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/projects/lsegal%2Faviary/settings");

	const agent = page.locator('[data-field="agent"]');
	const models = page.getByTestId("job-models").locator("li");
	await expect(agent).toHaveValue("coder");
	await expect(models).toHaveCount(2);
	await expect(models.nth(0)).toHaveAttribute("data-spec", "claude/opus");
	await expect(models.nth(0)).toContainText("model");
	await expect(models.nth(1)).toHaveAttribute("data-spec", "codex/gpt-5.5");
	await expect(models.nth(1)).toContainText("fallback");
	await expect(
		page.getByRole("link", { name: "coder's settings" }),
	).toHaveAttribute("href", "/settings/agents/coder");

	// An agent on an API model runs the job itself.
	await agent.selectOption("atlas");
	await expect(models).toHaveCount(1);
	await expect(models.nth(0)).toHaveAttribute("data-spec", "aviary/atlas");
	await expect(models.nth(0)).toContainText("runs on the agent itself");
	const yaml = page.getByTestId("project-yaml");
	await expect(yaml).toContainText("agent: atlas");
	expect(calls.config_save).toBeUndefined();
});

test("a CLI provider model without the cli/ prefix runs jobs on that CLI", async ({
	page,
}) => {
	await setup(page, {
		config: {
			...CONFIG,
			agents: [
				{
					name: "coder",
					model: "claude/opus",
					fallbacks: ["gemini/gemini-3.5-flash", "anthropic/claude-sonnet-4-5"],
				},
			],
		},
	});
	await page.goto("/projects/lsegal%2Faviary/settings");

	const models = page.getByTestId("job-models").locator("li");
	await expect(models).toHaveCount(3);
	await expect(models.nth(0)).toHaveAttribute("data-spec", "claude/opus");
	await expect(models.nth(1)).toHaveAttribute(
		"data-spec",
		"gemini/gemini-3.5-flash",
	);
	await expect(models.nth(2)).toHaveAttribute("data-spec", "aviary/coder");
});

test("a project without its own agent uses the shared one", async ({
	page,
}) => {
	await setup(page, {
		config: {
			server: {},
			agents: CONFIG.agents,
			projects: { agent: "coder" },
		},
	});
	await page.goto("/projects/lsegal%2Faviary/settings");
	await expect(page.getByTestId("agent-shared")).toBeVisible();
	await expect(page.locator('[data-field="agent"]')).toHaveValue("");
	await expect(page.locator('[data-field="agent"] option').first()).toHaveText(
		"Shared: coder",
	);
	await expect(
		page.getByTestId("job-models").locator("li").first(),
	).toHaveAttribute("data-spec", "claude/opus");
	await expect(page.getByTestId("project-yaml")).toHaveText(
		"projects:\n  agent: coder",
	);
});

test("a project attached to no runnable agent says why", async ({ page }) => {
	await setup(page, {
		config: {
			server: {},
			agents: CONFIG.agents,
			projects: { list: [{ target: "lsegal/aviary", agent: "gone" }] },
		},
	});
	await page.goto("/projects/lsegal%2Faviary/settings");
	await expect(page.getByTestId("agent-problem")).toHaveText(
		'Agent "gone" is not configured.',
	);
	await page.locator('[data-field="agent"]').selectOption("");
	await expect(page.getByTestId("agent-problem")).toContainText(
		"models.defaults names no CLI model",
	);
});

test("clones show what's in use and unused ones can be cleared", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/projects/lsegal%2Faviary/settings");
	const rows = page.getByTestId("clone-row");
	await expect(rows).toHaveCount(3);
	await expect(rows.first()).toContainText("lsegal/aviary#209");
	await expect(rows.first().getByTestId("clone-state")).toHaveText(
		"unused: not owned by this instance: no job here records it",
	);
	await expect(rows.nth(1)).toContainText("PR #233");
	await expect(rows.nth(1).getByTestId("clone-state")).toHaveText(
		"in use: job is paused",
	);
	await expect(rows.nth(2).getByTestId("clone-state")).toHaveText(
		"kept: outside the configured temp directories; remove it by hand",
	);
	await expect(rows.first().getByTestId("clone-size")).toHaveText("150 MB");
	await expect(rows.nth(1).getByTestId("clone-size")).toHaveText(
		"size unknown",
	);
	await expect(page.getByTestId("clone-total")).toHaveText(
		"3 clones, 150 MB total (1 not measured)",
	);

	await page.getByRole("button", { name: "Clear unused clones" }).click();
	await expect(page.getByTestId("clone-message")).toHaveText(
		"Removed 1 clone, reclaimed 150 MB, kept 2.",
	);
	expect(calls.project_clone_clear).toEqual([{}]);
});

test("an unreadable work state shows an error instead of clones", async ({
	page,
}) => {
	await setup(page, {
		clonesError: "work state could not be read: unexpected end of JSON",
	});
	await page.goto("/projects/lsegal%2Faviary/settings");
	await expect(
		page.getByText(
			"Couldn't list clones: work state could not be read: unexpected end of JSON",
		),
	).toBeVisible();
	await expect(page.getByTestId("clone-row")).toHaveCount(0);
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
	expect(calls.project_add).toEqual([
		{ target: "lsegal/zvidlib", runtime: true },
	]);
});

test("a new project with its own settings is written before it starts", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/projects/lsegal%2Fzvidlib/settings?add=1");
	await page.locator('[data-field="agent"]').selectOption("atlas");
	await page.getByRole("button", { name: "Save and start watching" }).click();
	await expect(page).toHaveURL("/projects/lsegal%2Fzvidlib");
	expect(savedSection(calls).list).toEqual([
		CONFIG.projects.list[0],
		{ target: "lsegal/zvidlib", agent: "atlas" },
	]);
	// Saved to the list, so the config watcher starts it; no runtime watch.
	expect(calls.project_add).toBeUndefined();
});

test("saving keeps a project's other per-project settings", async ({
	page,
}) => {
	const other = { mode: "future" };
	const calls = await setup(page, {
		config: {
			server: {},
			agents: CONFIG.agents,
			projects: { list: [{ target: "lsegal/aviary", other }] },
		},
	});
	await page.goto("/projects/lsegal%2Faviary/settings");
	await page.locator('[data-field="agent"]').selectOption("atlas");
	await page.getByRole("button", { name: "Save", exact: true }).click();
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedSection(calls).list).toEqual([
		{ target: "lsegal/aviary", other, agent: "atlas" },
	]);
});

const REPORTS_CONFIG = {
	...CONFIG,
	agents: [
		{
			name: "atlas",
			model: "anthropic/claude-sonnet-4-5",
			channels: [{ type: "slack", id: "main", token: "xoxb-test" }],
		},
		{ name: "scout", model: "anthropic/claude-sonnet-4-5" },
	],
};

test("reports can be turned on with an agent and a target, then off", async ({
	page,
}) => {
	const calls = await setup(page, { config: REPORTS_CONFIG });
	await page.goto("/projects/lsegal%2Faviary/settings");
	const yaml = page.getByTestId("project-yaml");
	const save = page.getByRole("button", { name: "Save", exact: true });
	const toggle = page.getByRole("switch", { name: "Send job reports" });
	await expect(toggle).toHaveAttribute("aria-checked", "false");
	await expect(page.locator('[data-field="reports_agent"]')).toHaveCount(0);

	await toggle.click();
	await expect(page.getByTestId("reports-error")).toHaveText(
		"Choose the Aviary agent that sends reports.",
	);
	await expect(save).toBeDisabled();
	await page.locator('[data-field="reports_agent"]').selectOption("atlas");
	await expect(page.getByTestId("reports-error")).toHaveText(
		"Reports need at least one target.",
	);

	await page
		.getByRole("combobox", { name: "Target 1 channel" })
		.selectOption("slack:main");
	await page
		.getByRole("textbox", { name: "Target 1 delivery ID" })
		.fill("C123");
	await page.getByRole("button", { name: "Add a target" }).click();
	await page
		.getByRole("combobox", { name: "Target 2 channel" })
		.selectOption("session");
	await page
		.getByRole("textbox", { name: "Target 2 delivery ID" })
		.fill("projects");
	await page.locator('[data-field="report_waiting"]').uncheck();
	await expect(page.getByTestId("reports-error")).toHaveCount(0);
	await expect(yaml).toContainText(
		[
			"    - target: lsegal/aviary",
			"      agent: coder",
			"      reports:",
			"        agent: atlas",
			'        targets: ["slack:main:C123", "session:projects"]',
			"        events: [merged, failed]",
		].join("\n"),
	);

	await save.click();
	await expect(page.getByTestId("save-note")).toHaveText(
		"Saved and applied to the running watch",
	);
	expect(savedSection(calls).list).toEqual([
		{
			target: "lsegal/aviary",
			agent: "coder",
			reports: {
				agent: "atlas",
				targets: ["slack:main:C123", "session:projects"],
				events: ["merged", "failed"],
			},
		},
	]);

	// Turning reports off removes the key again.
	await toggle.click();
	await expect(yaml).not.toContainText("reports:");
	await save.click();
	await expect.poll(() => calls.config_save?.length).toBe(2);
	expect(savedSection(calls)).toEqual(CONFIG.projects);
});

test("saved reports load into the form and are checked like the server does", async ({
	page,
}) => {
	await setup(page, {
		config: {
			...REPORTS_CONFIG,
			projects: {
				list: [
					{
						target: "lsegal/aviary",
						reports: { agent: "atlas", targets: ["slack:main:C123"] },
					},
				],
			},
		},
	});
	await page.goto("/projects/lsegal%2Faviary/settings");
	await expect(
		page.getByRole("switch", { name: "Send job reports" }),
	).toHaveAttribute("aria-checked", "true");
	await expect(page.locator('[data-field="reports_agent"]')).toHaveValue(
		"atlas",
	);
	await expect(
		page.getByRole("textbox", { name: "Target 1 delivery ID" }),
	).toHaveValue("C123");
	// No events filter means every event is reported.
	for (const e of ["merged", "failed", "waiting"])
		await expect(page.locator(`[data-field="report_${e}"]`)).toBeChecked();

	for (const e of ["merged", "failed", "waiting"])
		await page.locator(`[data-field="report_${e}"]`).uncheck();
	await expect(page.getByTestId("reports-error")).toHaveText(
		"Choose at least one event to report.",
	);
	await expect(
		page.getByRole("button", { name: "Save", exact: true }),
	).toBeDisabled();
	await page.locator('[data-field="report_failed"]').check();
	await page.getByRole("button", { name: "Remove target 1" }).click();
	await expect(page.getByTestId("reports-error")).toHaveText(
		"Reports need at least one target.",
	);
});

test("a project is deleted after a confirmation that names it and its in-flight jobs", async ({
	page,
}) => {
	const calls = await setup(page, {
		jobs: [
			{ number: 12, status: "active" },
			{ number: 14, status: "paused" },
			{ number: 15, status: "complete" },
		],
	});
	await page.goto("/projects/lsegal%2Faviary/settings");
	await page.getByRole("button", { name: "Delete project" }).click();
	const dialog = page.getByRole("alertdialog");
	await expect(dialog.getByRole("heading")).toHaveText("Delete lsegal/aviary?");
	await expect(dialog).toContainText("clones on disk are not touched");
	await expect(dialog.getByTestId("delete-jobs")).toContainText(
		"2 jobs are still in flight (#12, #14). Deleting stops them",
	);
	expect(calls.project_job_list?.at(-1)).toMatchObject({
		target: "lsegal/aviary",
	});

	// Cancelling removes nothing.
	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect(dialog).toBeHidden();
	expect(calls.project_remove).toBeUndefined();

	await page.getByRole("button", { name: "Delete project" }).click();
	await dialog.getByRole("button", { name: "Delete project" }).click();
	await expect(page).toHaveURL(/\/projects$/);
	expect(calls.project_remove).toEqual([{ target: "lsegal/aviary" }]);
	// The removal goes through project_remove alone, never a config rewrite.
	expect(calls.config_save).toBeUndefined();
});

test("a project being added has nothing to delete", async ({ page }) => {
	await setup(page);
	await page.goto("/projects/lsegal%2Fzvidlib/settings?add=1");
	await expect(page.locator("#general")).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Delete project" }),
	).toHaveCount(0);
	await expect(page.getByRole("link", { name: "Delete" })).toHaveCount(0);
});
