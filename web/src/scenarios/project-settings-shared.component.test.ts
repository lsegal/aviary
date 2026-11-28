// Code Loop settings: the shared projects: section, issue discovery modes,
// job models and each agent's project list, ported from
// web/e2e/project-settings.spec.ts (#1636).
import { expect, test } from "vitest";
import { commands, page } from "vitest/browser";
import { currentURL, goto, renderApp } from "../component-test/app";
import { chooseValue, expectValue } from "./helpers/select";
import {
	CODER_PROJECTS,
	CONFIG,
	count,
	PROJECTS,
	REPORTS_CONFIG,
	saveChanges,
	savedConfig,
	savedProjects,
	setChecked,
	setup,
} from "./project-settings.fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/ProjectsView.vue";
import "../views/SettingsView.vue";

/** Each matched element's text, whitespace collapsed, as Playwright's
 * toHaveText with an array compares them. */
function texts(loc: ReturnType<typeof page.getByTestId>): string[] {
	return loc
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
}

test("a project's issue discovery mode is set in its settings", async () => {
	const calls = setup();
	await renderApp("/projects/settings?agent=coder");
	const card = page.getByTestId("agent-project-mode");
	// Only Watch and Manual, with the effective mode checked (#1461).
	await expect.poll(() => count(card.getByRole("radio"))).toBe(2);
	await expect
		.element(card)
		.not.toMatchTextContent(/inherit|overridden|reset to global/i);
	await expect
		.element(card.getByRole("radio", { name: /Watch issues/ }))
		.toBeChecked();
	await setChecked(
		card.getByRole("radio", { name: /Manual: only what I queue/ }),
	);
	await expect
		.element(page.getByTestId("agent-project-yaml"))
		.toMatchTextContent("mode: manual");
	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedProjects(calls, "coder")[0].mode).toBe("manual");

	// Watch is the global mode, so picking it removes the override.
	await setChecked(card.getByRole("radio", { name: /Watch issues/ }));
	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(2);
	expect(savedProjects(calls, "coder")[0]).not.toHaveProperty("mode");
});

test("the global issue discovery mode flips projects without an override", async () => {
	const calls = setup();
	await renderApp("/projects/settings?agent=coder");
	const global = page
		.getByTestId("project-default-mode")
		.locator('[data-field="mode"]');
	await expect
		.element(page.getByTestId("project-default-mode"))
		.toMatchTextContent("Each project can override it");
	await chooseValue(global, "manual");
	const card = page.getByTestId("agent-project-mode");
	await expect
		.element(card.getByRole("radio", { name: /Manual: only what I queue/ }))
		.toBeChecked();
	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedConfig(calls).projects?.mode).toBe("manual");
	expect(savedProjects(calls, "coder")[0]).not.toHaveProperty("mode");
});

test("finished_ttl is set globally and overridden per project (#1460)", async () => {
	const calls = setup();
	await renderApp("/projects/settings?agent=coder");
	const card = page.getByTestId("agent-project-finished-ttl");
	const source = card.getByTestId("project-finished-ttl-source");
	await expect
		.element(source)
		.toMatchTextContent("Inherited from the global setting: 2w");

	// The global value is what projects inherit.
	await page
		.getByTestId("project-default-finished-ttl")
		.locator('[data-field="finished_ttl"]')
		.fill("10d");
	await expect
		.element(source)
		.toMatchTextContent("Inherited from the global setting: 10d");
	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedConfig(calls).projects?.finished_ttl).toBe("10d");

	// A project's own value overrides it, and Reset to global removes it.
	const input = card.locator('[data-field="finished_ttl"]');
	await input.fill("3d");
	(input.element() as HTMLElement).blur();
	await expect
		.element(source)
		.toMatchTextContent("Overridden for this project: 3d (global: 10d)");
	await expect
		.element(page.getByTestId("agent-project-yaml"))
		.toMatchTextContent('finished_ttl: "3d"');
	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(2);
	expect(savedProjects(calls, "coder")[0].finished_ttl).toBe("3d");
	await card.getByTestId("project-finished-ttl-reset").click();
	await expect
		.element(source)
		.toMatchTextContent("Inherited from the global setting");
	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(3);
	expect(savedProjects(calls, "coder")[0]).not.toHaveProperty("finished_ttl");
});

test("a project's board links to its projects in its agent's Code Loop settings", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	// The breadcrumb names the agent, so the header has no agent chip (#1570).
	await expect.element(page.getByTestId("delivery-status")).toBeVisible();
	await expect.poll(() => count(page.getByTestId("project-owner"))).toBe(0);
	await expect
		.element(page.getByTestId("settings-header"))
		.not.toMatchTextContent("agent:");
	// The header's actions are icon buttons, Settings rightmost, and there is
	// no Add project button (projects are added under their agent).
	const header = page.getByTestId("settings-header");
	const more = header.getByRole("button", { name: "More board actions" });
	await expect.element(more).toHaveAttribute("title", "More board actions");
	await expect.element(more).toHaveText("");
	const chatToggle = header.getByRole("button", { name: "Prompt chat" });
	await expect.element(chatToggle).toHaveAttribute("title", "Prompt chat");
	await expect.element(chatToggle).toHaveText("");
	await expect
		.poll(() => count(header.getByRole("button", { name: /Add project/ })))
		.toBe(0);
	const actions = header.locator("a, button");
	await expect
		.element(actions.last())
		.toHaveAttribute("data-testid", "board-more-menu");
	// Settings is a labeled item in the "…" menu (#1488).
	await more.click();
	const settings = page.getByRole("menuitem", { name: "Settings" });
	await expect
		.poll(() => settings.query()?.getAttribute("href"))
		.toMatch(/\/projects\/settings/);
	await settings.click();
	await expect
		.poll(currentURL)
		.toBe("/projects/settings?agent=coder&project=lsegal%2Faviary");
	await expect
		.element(page.getByRole("tab", { name: "coder", selected: true }))
		.toBeVisible();
	const projects = page.getByTestId("agent-project");
	await expect.poll(() => count(projects)).toBe(1);
	await expect
		.element(projects.first())
		.toHaveAttribute("data-target", "lsegal/aviary");
	await expect
		.element(page.getByTestId("agent-project-list"))
		.toMatchTextContent("GitHub projects coder owns");
});

test("changing a setting writes only the differing values to the shared section", async () => {
	const calls = setup();
	await renderApp("/projects/settings");

	const yaml = page.getByTestId("project-yaml");
	await expect
		.element(yaml)
		.toHaveText(
			[
				"projects:",
				"  allowed_commenters: [lsegal, teammate]",
				"  webhook:",
				"    path: /hooks/gh",
			].join("\n"),
		);
	const concurrency = page.locator('[data-field="concurrency"]');
	const jobsLabel = page.locator("label").filter({ has: concurrency });
	await expect.element(jobsLabel).toMatchTextContent("default");
	await expect
		.element(page.locator('[data-field="allowed_commenters"]'))
		.toHaveValue("lsegal, teammate");

	const merge = page.getByRole("switch", {
		name: "Merge pull requests when CI passes",
	});
	const vision = page.getByRole("switch", {
		name: "Read pages it no longer recognizes from capped screenshots",
	});
	const delivery = page.locator('[data-field="delivery"]');
	const interval = page.locator('[data-field="interval"]');

	await concurrency.fill("6");
	await expect.element(jobsLabel).not.toMatchTextContent("default");
	await merge.click();
	// Browser delivery is the default and the only mode with browser switches.
	await expectValue(delivery, "browser");
	await expect.element(interval).toHaveAttribute("placeholder", "20s");
	await vision.click();
	await expect.element(yaml).toMatchTextContent("browser_vision: true");
	await chooseValue(delivery, "webhook");
	await expect
		.poll(() =>
			count(
				page.getByRole("switch", {
					name: "Show the browser it reads GitHub with, for debugging",
				}),
			),
		)
		.toBe(0);
	await expect.element(interval).toHaveAttribute("placeholder", "30s");
	await expect.element(yaml).not.toMatchTextContent("browser_vision");
	await expect.element(yaml).toMatchTextContent("concurrency: 6");
	await expect.element(yaml).toMatchTextContent("no_merge: true");
	await expect.element(yaml).toMatchTextContent("delivery: webhook");

	await saveChanges();
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
	await expect
		.element(page.getByRole("button", { name: "Save Changes" }))
		.toBeEnabled();
	await concurrency.fill("3");
	await merge.click();
	await chooseValue(delivery, "browser");
	await vision.click();
	await expect.element(yaml).not.toMatchTextContent("concurrency:");
	await expect.element(yaml).not.toMatchTextContent("delivery:");
	await expect.element(yaml).not.toMatchTextContent("browser_vision");
	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(2);
	expect(savedConfig(calls).projects).toEqual(PROJECTS);
	expect(savedProjects(calls, "coder")).toEqual(CODER_PROJECTS);
});

test("Code Loop settings holds each agent's projects beside the shared settings", async () => {
	setup({ config: REPORTS_CONFIG });
	await renderApp("/projects/settings");
	await expect.element(page.getByTestId("project-settings")).toBeVisible();
	const agents = page.getByTestId("agent-projects-agent");
	await expect
		.poll(() => texts(agents))
		.toEqual(["atlas 0", "scout 0", "coder 1"]);
	// The first agent is shown until another is picked.
	await expect
		.element(page.getByTestId("agent-project-list"))
		.toMatchTextContent("atlas owns no projects yet.");
	await agents.nth(2).click();
	await expect.poll(currentURL).toBe("/projects/settings?agent=coder");
	await expect.poll(() => count(page.getByTestId("agent-project"))).toBe(1);
	await page.getByTestId("agent-board-link").click();
	await expect.poll(currentURL).toBe("/agents/coder/projects");
});

test("yolo loads as the full approval mode and saves as the Projects override", async () => {
	const calls = setup({
		config: { ...CONFIG, projects: { ...PROJECTS, yolo: true } },
	});
	await renderApp("/projects/settings");
	const mode = page.locator('[data-field="approval_mode"]');
	await expectValue(mode, "full");

	await chooseValue(mode, "read-only");
	const yaml = page.getByTestId("project-yaml");
	await expect.element(yaml).toMatchTextContent("approval_mode: read-only");
	await expect.element(yaml).not.toMatchTextContent("yolo");
	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedConfig(calls).projects).toEqual({
		...PROJECTS,
		approval_mode: "read-only",
	});
});

test("following the general approval mode writes no Projects override", async () => {
	const calls = setup({
		config: { ...CONFIG, projects: { ...PROJECTS, approval_mode: "auto" } },
	});
	await renderApp("/projects/settings");
	const mode = page.locator('[data-field="approval_mode"]');
	await expectValue(mode, "auto");
	await chooseValue(mode, "");
	await expect
		.element(page.getByTestId("project-yaml"))
		.not.toMatchTextContent("approval_mode");
	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedConfig(calls).projects).toEqual(PROJECTS);
});

test("an agent's projects run their jobs on its model and fallbacks", async () => {
	setup();
	await renderApp("/projects/settings?agent=coder");
	const models = page.getByTestId("job-models").locator("li");
	await expect.poll(() => count(models)).toBe(2);
	await expect
		.element(models.nth(0))
		.toHaveAttribute("data-spec", "claude/opus");
	await expect.element(models.nth(0)).toMatchTextContent("model");
	await expect
		.element(models.nth(1))
		.toHaveAttribute("data-spec", "codex/gpt-5.5");
	await expect.element(models.nth(1)).toMatchTextContent("fallback");

	// An agent on an API model runs the job itself.
	await goto("/projects/settings?agent=atlas");
	await expect.poll(() => count(models)).toBe(1);
	await expect
		.element(models.nth(0))
		.toHaveAttribute("data-spec", "aviary/atlas");
	await expect
		.element(models.nth(0))
		.toMatchTextContent("runs on the agent itself");
	await expect
		.element(page.getByTestId("agent-project-list"))
		.toMatchTextContent("atlas owns no projects yet.");
});

test("a job model override replaces every agent's models for its projects' jobs", async () => {
	const calls = setup();
	await renderApp("/projects/settings?agent=coder");
	const models = page.getByTestId("job-models").locator("li");
	await expect
		.element(models.nth(0))
		.toHaveAttribute("data-spec", "claude/opus");
	await expect
		.element(page.getByTestId("job-models-source"))
		.toMatchTextContent("coder's model");

	// The picker offers the installed CLI providers' models.
	const model = page.locator('[data-field="model"]');
	await chooseValue(model, "claude/sonnet");
	const fallbacks = page.locator('[data-field="fallbacks"]');
	await fallbacks.click();
	await page
		.getByRole("listbox")
		.locator('[role="option"][data-value="codex/gpt-5.5-mini"]')
		.click();
	await commands.keyPress("Escape");

	const yaml = page.getByTestId("project-yaml");
	await expect.element(yaml).toMatchTextContent("model: claude/sonnet");
	await expect
		.element(yaml)
		.toMatchTextContent("fallbacks: [codex/gpt-5.5-mini]");
	await expect.poll(() => count(models)).toBe(2);
	await expect
		.element(models.nth(0))
		.toHaveAttribute("data-spec", "claude/sonnet");
	await expect
		.element(models.nth(1))
		.toHaveAttribute("data-spec", "codex/gpt-5.5-mini");
	await expect
		.element(page.getByTestId("job-models-source"))
		.toMatchTextContent("project override");

	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedConfig(calls).projects).toEqual({
		...PROJECTS,
		model: "claude/sonnet",
		fallbacks: ["codex/gpt-5.5-mini"],
	});
	expect(savedProjects(calls, "coder")).toEqual(CODER_PROJECTS);
});

test("a saved job model override loads into the picker", async () => {
	setup({
		config: {
			...CONFIG,
			projects: {
				...PROJECTS,
				model: "claude:opus",
				fallbacks: ["codex/gpt-5.5"],
			},
		},
	});
	await renderApp("/projects/settings?agent=atlas");
	await expectValue(page.locator('[data-field="model"]'), "claude:opus");
	await expect
		.poll(() =>
			count(
				page.locator('[data-field="fallbacks"]').getByTestId("combobox-chip"),
			),
		)
		.toBe(1);
	// Even an agent on an API model runs its projects' jobs on the override.
	const models = page.getByTestId("job-models").locator("li");
	await expect.poll(() => count(models)).toBe(2);
	await expect
		.element(models.nth(0))
		.toHaveAttribute("data-spec", "claude/opus");
	await expect
		.element(models.nth(1))
		.toHaveAttribute("data-spec", "codex/gpt-5.5");
});

test("a CLI provider model without the cli/ prefix runs jobs on that CLI", async () => {
	setup({
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
	await renderApp("/projects/settings?agent=coder");

	const models = page.getByTestId("job-models").locator("li");
	await expect.poll(() => count(models)).toBe(3);
	await expect
		.element(models.nth(0))
		.toHaveAttribute("data-spec", "claude/opus");
	await expect
		.element(models.nth(1))
		.toHaveAttribute("data-spec", "gemini/gemini-3.5-flash");
	await expect
		.element(models.nth(2))
		.toHaveAttribute("data-spec", "aviary/coder");
});

test("projects are added to an agent in its settings", async () => {
	const calls = setup();
	await renderApp("/projects/settings?agent=atlas");
	const field = page.locator('[data-field="new_project"]');
	const add = page.getByRole("button", { name: "Add", exact: true });

	// coder already owns lsegal/aviary, and a project has exactly one agent.
	await field.fill("lsegal/aviary");
	await expect
		.element(page.getByTestId("new-project-check"))
		.toMatchTextContent("already belongs to agent coder");
	await expect.element(add).toBeDisabled();

	await field.fill("https://github.com/lsegal/zvidlib");
	await expect
		.element(page.getByTestId("new-project-check"))
		.toMatchTextContent("pull requests will come from your fork");
	// The draft's models for atlas, saved or not, pick the CLIs checked.
	expect(calls.project_check?.at(-1)).toMatchObject({
		agent: "atlas",
		models: ["anthropic/claude-sonnet-4-5"],
	});
	await add.click();
	const projects = page.getByTestId("agent-project");
	await expect.poll(() => count(projects)).toBe(1);
	await expect
		.element(projects.first())
		.toHaveAttribute("data-target", "lsegal/zvidlib");
	// Not saved yet, so it can simply be dropped again.
	await page.getByRole("button", { name: "Remove lsegal/zvidlib" }).click();
	await expect.poll(() => count(projects)).toBe(0);
	await field.fill("lsegal/zvidlib");
	await expect.element(add).toBeEnabled();
	await add.click();
	await saveChanges();
	await expect.poll(() => calls.config_save?.length).toBe(1);
	expect(savedProjects(calls, "atlas")).toEqual([{ target: "lsegal/zvidlib" }]);
	expect(savedProjects(calls, "coder")).toEqual(CODER_PROJECTS);
	// Once saved, its ✕ deletes it through project_remove instead.
	await expect
		.poll(() =>
			count(page.getByRole("button", { name: "Remove lsegal/zvidlib" })),
		)
		.toBe(0);
	await expect
		.element(page.getByRole("button", { name: "Delete lsegal/zvidlib" }))
		.toBeVisible();
});
