import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

const STATUS = {
	identity: "7F3CA91B",
	ready_label: "ready",
	projects: [{ name: "lsegal/aviary", running: 0, waiting: 0, jobs: 0 }],
};

// Shapes follow internal/config/projects.go and internal/projects/api.go.
// A project lives in the projects: list of the agent that owns it; the
// settings every project runs under are the one top-level projects: section.
const CODER_PROJECTS = [{ target: "lsegal/aviary" }];
const PROJECTS = {
	allowed_commenters: ["lsegal", "teammate"],
	webhook: { path: "/hooks/gh" },
};
const CONFIG = {
	server: { port: 16677 },
	agents: [
		{ name: "atlas", model: "anthropic/claude-sonnet-4-5" },
		{
			name: "coder",
			model: "cli/claude/opus",
			fallbacks: ["cli/codex/gpt-5.5"],
			projects: CODER_PROJECTS,
		},
	],
	projects: PROJECTS,
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
		auth_list: [],
		session_list: [],
		tool_list: [],
		agent_file_list: [],
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
			if (opts.check) return opts.check;
			// The checks normalize the target, as project_check does.
			const target = String(args?.target ?? "").replace(
				"https://github.com/",
				"",
			);
			return { ...READY_CHECK, target };
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

/** The saved config: what the last config_save wrote. */
function savedConfig(calls: Calls) {
	return JSON.parse(String(calls.config_save?.at(-1)?.config));
}

/** The projects: list the last config_save wrote under agent. */
function savedProjects(calls: Calls, agent: string) {
	return savedConfig(calls).agents.find(
		(a: { name: string }) => a.name === agent,
	)?.projects;
}

const saveChanges = (page: Page) =>
	page.getByRole("button", { name: "Save Changes" }).click();

test("adding a project from the single field saves it to the chosen agent", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	await page.getByRole("button", { name: "+ Add project" }).click();

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
	// The board's agent owns it unless another is chosen.
	const agent = dialog.getByLabel("Agent that owns it");
	await expect(agent).toHaveValue("coder");
	await agent.selectOption("atlas");
	await expect(dialog).toContainText("Runs on atlas: aviary/atlas");
	await agent.selectOption("coder");
	await expect(dialog).toContainText(
		"Runs on coder: claude/opus, codex/gpt-5.5",
	);
	await expect(dialog).toContainText("Up to 3 jobs at once");
	await expect(dialog.locator("footer code")).toHaveText(
		"aviary project add lsegal/zvidlib --agent coder",
	);

	await dialog.getByRole("button", { name: "Start watching" }).click();
	await expect(page).toHaveURL("/agents/coder/projects/lsegal%2Fzvidlib");
	await expect(dialog).toBeHidden();
	expect(calls.project_add).toEqual([
		{ target: "lsegal/zvidlib", agent: "coder" },
	]);
	expect(calls.project_check?.at(-1)).toEqual({
		target: "https://github.com/lsegal/zvidlib",
	});
});

test("a project another agent owns cannot be added again", async ({ page }) => {
	const calls = await setup(page, {
		check: { ...READY_CHECK, target: "lsegal/aviary", fork: false },
	});
	await page.goto("/agents/coder/projects/lsegal%2Faviary?add=1");
	const dialog = page.getByRole("dialog");
	await dialog.getByLabel("Repository or project board").fill("lsegal/aviary");
	await expect(dialog.getByTestId("add-project-owned")).toHaveText(
		"lsegal/aviary already belongs to agent coder.",
	);
	await expect(
		dialog.getByRole("button", { name: "Start watching" }),
	).toBeDisabled();
	expect(calls.project_add).toBeUndefined();
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
	await page.goto("/agents/coder/projects/lsegal%2Faviary?add=1");
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
	await expect(page).toHaveURL("/agents/coder/projects/lsegal%2Faviary");
	expect(calls.project_add).toBeUndefined();
});

test("a project's board names its agent and links to its projects in System → Projects", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary");
	const owner = page.getByTestId("project-owner");
	await expect(owner).toHaveText("agent: coder");
	await page.getByTestId("project-settings-link").click();
	await expect(page).toHaveURL(
		"/system/projects?agent=coder&project=lsegal%2Faviary",
	);
	await expect(
		page.getByRole("tab", { name: "coder", selected: true }),
	).toBeVisible();
	const projects = page.getByTestId("agent-project");
	await expect(projects).toHaveCount(1);
	await expect(projects.first()).toHaveAttribute(
		"data-target",
		"lsegal/aviary",
	);
	await expect(page.getByTestId("agent-project-list")).toContainText(
		"GitHub projects coder owns",
	);
});

test("changing a setting writes only the differing values to the shared section", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/system/projects");

	const yaml = page.getByTestId("project-yaml");
	await expect(yaml).toHaveText(
		[
			"projects:",
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

	await saveChanges(page);
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedConfig(calls).projects).toEqual({
		...PROJECTS,
		concurrency: 6,
		no_merge: true,
		delivery: "webhook",
	});
	// No agent's section carries settings: the agents keep only their lists.
	expect(savedProjects(calls, "coder")).toEqual(CODER_PROJECTS);
	expect(savedProjects(calls, "atlas")).toBeUndefined();

	// Back to defaults, the values drop out again, once the save has reloaded
	// the draft.
	await expect(
		page.getByRole("button", { name: "Save Changes" }),
	).toBeEnabled();
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
	await saveChanges(page);
	await expect.poll(() => calls.config_save?.length).toBe(2);
	expect(savedConfig(calls).projects).toEqual(PROJECTS);
	expect(savedProjects(calls, "coder")).toEqual(CODER_PROJECTS);
});

test("System → Projects holds each agent's projects beside the shared settings", async ({
	page,
}) => {
	await setup(page, { config: REPORTS_CONFIG });
	await page.goto("/system/projects");
	await expect(page.getByTestId("project-settings")).toBeVisible();
	const agents = page.getByTestId("agent-projects-agent");
	await expect(agents).toHaveText(["atlas 0", "scout 0", "coder 1"]);
	// The first agent is shown until another is picked.
	await expect(page.getByTestId("agent-project-list")).toContainText(
		"atlas owns no projects yet.",
	);
	await agents.nth(2).click();
	await expect(page).toHaveURL("/system/projects?agent=coder");
	await expect(page.getByTestId("agent-project")).toHaveCount(1);
	await page.getByTestId("agent-board-link").click();
	await expect(page).toHaveURL("/agents/coder/projects");
});

test("yolo loads as the full approval mode and saves as the Projects override", async ({
	page,
}) => {
	const calls = await setup(page, {
		config: {
			...CONFIG,
			projects: { ...PROJECTS, yolo: true },
		},
	});
	await page.goto("/system/projects");
	const mode = page.locator('[data-field="approval_mode"]');
	await expect(mode).toHaveValue("full");

	await mode.selectOption("read-only");
	const yaml = page.getByTestId("project-yaml");
	await expect(yaml).toContainText("approval_mode: read-only");
	await expect(yaml).not.toContainText("yolo");
	await saveChanges(page);
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedConfig(calls).projects).toEqual({
		...PROJECTS,
		approval_mode: "read-only",
	});
});

test("following the general approval mode writes no Projects override", async ({
	page,
}) => {
	const calls = await setup(page, {
		config: {
			...CONFIG,
			projects: { ...PROJECTS, approval_mode: "auto" },
		},
	});
	await page.goto("/system/projects");
	const mode = page.locator('[data-field="approval_mode"]');
	await expect(mode).toHaveValue("auto");
	await mode.selectOption("");
	await expect(page.getByTestId("project-yaml")).not.toContainText(
		"approval_mode",
	);
	await saveChanges(page);
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedConfig(calls).projects).toEqual(PROJECTS);
});

test("an agent's projects run their jobs on its model and fallbacks", async ({
	page,
}) => {
	await setup(page);
	await page.goto("/system/projects?agent=coder");
	const models = page.getByTestId("job-models").locator("li");
	await expect(models).toHaveCount(2);
	await expect(models.nth(0)).toHaveAttribute("data-spec", "claude/opus");
	await expect(models.nth(0)).toContainText("model");
	await expect(models.nth(1)).toHaveAttribute("data-spec", "codex/gpt-5.5");
	await expect(models.nth(1)).toContainText("fallback");

	// An agent on an API model runs the job itself.
	await page.goto("/system/projects?agent=atlas");
	await expect(models).toHaveCount(1);
	await expect(models.nth(0)).toHaveAttribute("data-spec", "aviary/atlas");
	await expect(models.nth(0)).toContainText("runs on the agent itself");
	await expect(page.getByTestId("agent-project-list")).toContainText(
		"atlas owns no projects yet.",
	);
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
					projects: CODER_PROJECTS,
				},
			],
		},
	});
	await page.goto("/system/projects?agent=coder");

	const models = page.getByTestId("job-models").locator("li");
	await expect(models).toHaveCount(3);
	await expect(models.nth(0)).toHaveAttribute("data-spec", "claude/opus");
	await expect(models.nth(1)).toHaveAttribute(
		"data-spec",
		"gemini/gemini-3.5-flash",
	);
	await expect(models.nth(2)).toHaveAttribute("data-spec", "aviary/coder");
});

test("projects are added to an agent in its settings", async ({ page }) => {
	const calls = await setup(page);
	await page.goto("/system/projects?agent=atlas");
	const field = page.locator('[data-field="new_project"]');
	const add = page.getByRole("button", { name: "Add", exact: true });

	// coder already owns lsegal/aviary, and a project has exactly one agent.
	await field.fill("lsegal/aviary");
	await expect(page.getByTestId("new-project-check")).toContainText(
		"already belongs to agent coder",
	);
	await expect(add).toBeDisabled();

	await field.fill("https://github.com/lsegal/zvidlib");
	await expect(page.getByTestId("new-project-check")).toContainText(
		"pull requests will come from your fork",
	);
	await add.click();
	const projects = page.getByTestId("agent-project");
	await expect(projects).toHaveCount(1);
	await expect(projects.first()).toHaveAttribute(
		"data-target",
		"lsegal/zvidlib",
	);
	// Not saved yet, so it can simply be dropped again.
	await page.getByRole("button", { name: "Remove lsegal/zvidlib" }).click();
	await expect(projects).toHaveCount(0);
	await field.fill("lsegal/zvidlib");
	await expect(add).toBeEnabled();
	await add.click();
	await saveChanges(page);
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedProjects(calls, "atlas")).toEqual([{ target: "lsegal/zvidlib" }]);
	expect(savedProjects(calls, "coder")).toEqual(CODER_PROJECTS);
	// Once saved, its ✕ deletes it through project_remove instead.
	await expect(
		page.getByRole("button", { name: "Remove lsegal/zvidlib" }),
	).toHaveCount(0);
	await expect(
		page.getByRole("button", { name: "Delete lsegal/zvidlib" }),
	).toBeVisible();
});

test("clones show what's in use and unused ones can be cleared", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/system/projects");
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

test("System → Projects shows this instance's ID", async ({ page }) => {
	await setup(page);
	await page.goto("/system/projects");
	const card = page.getByTestId("projects-instance-id");
	await expect(card).toContainText("Instance ID");
	await expect(card.locator("code").first()).toHaveText("7F3CA91B");
	await expect(card).toContainText("/aviary:7F3CA91B");
});

test("an unreadable work state shows an error instead of clones", async ({
	page,
}) => {
	await setup(page, {
		clonesError: "work state could not be read: unexpected end of JSON",
	});
	await page.goto("/system/projects");
	await expect(
		page.getByText(
			"Couldn't list clones: work state could not be read: unexpected end of JSON",
		),
	).toBeVisible();
	await expect(page.getByTestId("clone-row")).toHaveCount(0);
});

test("change settings first opens the agent's projects with the new one listed", async ({
	page,
}) => {
	const calls = await setup(page);
	await page.goto("/agents/coder/projects/lsegal%2Faviary?add=1");
	const dialog = page.getByRole("dialog");
	await dialog.getByLabel("Repository or project board").fill("lsegal/zvidlib");
	await expect(dialog.getByTestId("add-project-checks")).toContainText(
		"Ready to go",
	);
	await dialog.getByLabel("Agent that owns it").selectOption("coder");
	await dialog.getByRole("button", { name: "Change settings first" }).click();
	await expect(page).toHaveURL(
		"/system/projects?agent=coder&add=lsegal%2Fzvidlib",
	);
	await expect(page.getByTestId("agent-project")).toHaveCount(2);
	await expect(page.getByTestId("agent-project-yaml")).toContainText(
		"- target: lsegal/zvidlib",
	);
	await saveChanges(page);
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedProjects(calls, "coder")).toEqual([
		{ target: "lsegal/aviary" },
		{ target: "lsegal/zvidlib" },
	]);
	expect(calls.project_add).toBeUndefined();
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
		{
			name: "coder",
			model: "cli/claude/opus",
			projects: [{ target: "lsegal/aviary", other: "kept" }],
		},
	],
};

test("reports can be turned on with an agent and a target, then off", async ({
	page,
}) => {
	const calls = await setup(page, { config: REPORTS_CONFIG });
	await page.goto("/system/projects?agent=coder");
	const yaml = page.getByTestId("agent-project-yaml");
	const toggle = page.getByRole("switch", { name: "Send job reports" });
	await expect(toggle).toHaveAttribute("aria-checked", "false");
	await expect(page.locator('[data-field="reports_agent"]')).toHaveCount(0);

	await toggle.click();
	// Reports go through the owning agent unless another is chosen.
	await expect(page.locator('[data-field="reports_agent"]')).toHaveValue(
		"coder",
	);
	await expect(page.getByTestId("reports-error")).toContainText(
		"Reports need at least one target.",
	);
	await page.locator('[data-field="reports_agent"]').selectOption("atlas");

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
			"      - target: lsegal/aviary",
			"        other: kept",
			"        reports:",
			"          agent: atlas",
			'          targets: ["slack:main:C123", "session:projects"]',
			"          events: [merged, failed]",
		].join("\n"),
	);

	await saveChanges(page);
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedProjects(calls, "coder")).toEqual([
		{
			target: "lsegal/aviary",
			other: "kept",
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
	await saveChanges(page);
	await expect.poll(() => calls.config_save?.length).toBe(2);
	expect(savedProjects(calls, "coder")).toEqual(
		REPORTS_CONFIG.agents[2].projects,
	);
});

test("saved reports load into the form and are checked like the server does", async ({
	page,
}) => {
	await setup(page, {
		config: {
			...REPORTS_CONFIG,
			agents: [
				REPORTS_CONFIG.agents[0],
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: [
						{
							target: "lsegal/aviary",
							reports: { agent: "atlas", targets: ["slack:main:C123"] },
						},
					],
				},
			],
		},
	});
	await page.goto("/system/projects?agent=coder");
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
	await expect(page.getByTestId("reports-error")).toContainText(
		"Choose at least one event to report.",
	);
	await page.locator('[data-field="report_failed"]').check();
	await page.getByRole("button", { name: "Remove target 1" }).click();
	await expect(page.getByTestId("reports-error")).toContainText(
		"Reports need at least one target.",
	);
});

test("a project row's ✕ deletes that project after a confirmation naming it and its in-flight jobs", async ({
	page,
}) => {
	const calls = await setup(page, {
		config: {
			...CONFIG,
			agents: [
				CONFIG.agents[0],
				{
					...CONFIG.agents[1],
					projects: [{ target: "lsegal/aviary" }, { target: "lsegal/nupp" }],
				},
			],
		},
		jobs: [
			{ number: 12, status: "active" },
			{ number: 14, status: "paused" },
			{ number: 15, status: "complete" },
		],
	});
	await page.goto("/system/projects?agent=coder");
	const projects = page.getByTestId("agent-project");
	await expect(projects).toHaveCount(2);
	// Every saved row carries its own ✕; there is no separate Delete section.
	await expect(
		page.getByRole("button", { name: "Delete lsegal/aviary" }),
	).toBeVisible();
	await expect(page.locator("#delete")).toHaveCount(0);
	await expect(
		page.getByRole("button", { name: "Delete project" }),
	).toHaveCount(0);
	// A saved project is deleted with a confirmation, not dropped from the draft.
	await expect(
		page.getByRole("button", { name: "Remove lsegal/nupp" }),
	).toHaveCount(0);

	// The ✕ deletes its own row's project, not the selected one.
	await expect(
		page.getByRole("button", { name: "lsegal/aviary", exact: true }),
	).toHaveAttribute("aria-pressed", "true");
	await page.getByRole("button", { name: "Delete lsegal/nupp" }).click();
	const dialog = page.getByRole("alertdialog");
	await expect(dialog.getByRole("heading")).toHaveText("Delete lsegal/nupp?");
	await expect(dialog).toContainText("clones on disk are not touched");
	await expect(dialog.getByTestId("delete-jobs")).toContainText(
		"2 jobs are still in flight (#12, #14). Deleting stops them",
	);
	expect(calls.project_job_list?.at(-1)).toMatchObject({
		target: "lsegal/nupp",
	});

	// Cancelling removes nothing.
	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect(dialog).toBeHidden();
	expect(calls.project_remove).toBeUndefined();

	await page.getByRole("button", { name: "Delete lsegal/nupp" }).click();
	await dialog.getByRole("button", { name: "Delete project" }).click();
	await expect(dialog).toBeHidden();
	await expect(projects).toHaveCount(1);
	await expect(projects.first()).toHaveAttribute(
		"data-target",
		"lsegal/aviary",
	);
	expect(calls.project_remove).toEqual([{ target: "lsegal/nupp" }]);
	// The removal goes through project_remove alone, never a config rewrite.
	expect(calls.config_save).toBeUndefined();
});

test("a project being added has nothing to delete", async ({ page }) => {
	await setup(page);
	await page.goto("/system/projects?agent=coder&add=lsegal%2Fzvidlib");
	await expect(page.getByTestId("agent-project")).toHaveCount(2);
	await expect(
		page.getByRole("button", { name: "lsegal/zvidlib", exact: true }),
	).toHaveAttribute("aria-pressed", "true");
	await expect(
		page.getByRole("button", { name: "Delete lsegal/zvidlib" }),
	).toHaveCount(0);
	await expect(
		page.getByRole("button", { name: "Remove lsegal/zvidlib" }),
	).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Delete lsegal/aviary" }),
	).toBeVisible();
});
