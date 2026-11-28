// Agent task and file scenarios, ported from web/e2e/agents.spec.ts (#1636).
// Agent, channel and system settings live in
// agents-settings.component.test.ts.
import { beforeEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import {
	type Args,
	ASSISTANT,
	agentFileFixtures,
	CONFIG,
	count,
	mockDefault,
	TOOLS,
	texts,
} from "./agents.fixtures";
import {
	chooseOption,
	expectValue,
	optionLabels,
	searchOption,
} from "./helpers/select";
import {
	agentPageLink,
	openAgentPage,
	openTaskSettings,
} from "./helpers/sidebar";
// The pages this file renders (see component-test/routes.ts).
import "../views/OverviewView.vue";
import "../views/SettingsView.vue";

beforeEach(() => {
	mockDefault();
});

test("saving settings preserves task prompt newlines", async () => {
	let savedConfig: unknown = null;

	mockMCP({
		config_get: CONFIG,
		config_save: (args?: Args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}"));
			return "ok";
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [ASSISTANT],
		tool_list: TOOLS,
		...agentFileFixtures(),
	});

	await openAgentPage("settings");
	await agentPageLink("Scheduled Tasks").click();
	// The root page is the overview; a task's Settings opens it for editing.
	await openTaskSettings();

	const prompt = page.getByPlaceholder("Task prompt...").first();
	await prompt.fill("line 1\n\nline 3\n");
	await page.getByRole("button", { name: "Save Changes" }).click();

	await expect.element(page.getByTitle("Settings saved")).toBeVisible();
	expect(savedConfig).toMatchObject({
		agents: [{ tasks: [{ prompt: "line 1\n\nline 3\n" }] }],
	});
});

test("tasks can be enabled from the settings UI", async () => {
	await openAgentPage("settings");
	await agentPageLink("Scheduled Tasks").click();
	// The root page is the overview; a task's Settings opens it for editing.
	await openTaskSettings();

	const chip = page.getByTestId("paused-chip");
	await expect.element(chip).toHaveText("Paused");
	await expect
		.poll(() => count(page.getByText("disabled", { exact: true })))
		.toBe(0);
	const toggle = page
		.getByRole("switch", { name: "Toggle task enabled" })
		.first();
	await toggle.click();
	await expect.element(toggle).toHaveAttribute("aria-checked", "true");
	await expect.poll(() => count(chip)).toBe(0);
});

test("task header wraps cleanly on mobile", async () => {
	await page.viewport(390, 844);
	await renderApp("/agents/assistant/tasks?task=daily-briefing");

	const definedIn = page.getByText("Defined in: aviary.yaml", { exact: true });
	const enabledSwitch = page.getByRole("switch", {
		name: "Toggle task enabled",
	});

	await expect.element(definedIn).toBeVisible();
	await expect.element(enabledSwitch).toBeVisible();

	const definedInBox = definedIn.element().getBoundingClientRect();
	const enabledSwitchBox = enabledSwitch.element().getBoundingClientRect();

	expect(definedInBox.x).toBeGreaterThanOrEqual(0);
	expect(definedInBox.x + definedInBox.width).toBeLessThanOrEqual(390);
	expect(enabledSwitchBox.x).toBeGreaterThanOrEqual(0);
	expect(enabledSwitchBox.x + enabledSwitchBox.width).toBeLessThanOrEqual(390);

	expect(document.documentElement.scrollWidth).toBeLessThanOrEqual(
		document.documentElement.clientWidth,
	);
});

test("deleting a task from settings persists immediately", async () => {
	let savedConfig: unknown = null;

	mockMCP({
		config_get: {
			...CONFIG,
			agents: [
				{
					...CONFIG.agents[0],
					tasks: [
						{
							enabled: true,
							name: "file-task",
							prompt: "From file",
							from_file: true,
							file: "tasks/file-task.md",
						},
					],
				},
			],
		},
		config_save: (args?: Args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}"));
			return "ok";
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [ASSISTANT],
		tool_list: TOOLS,
		...agentFileFixtures(),
	});

	await openAgentPage("settings");
	await agentPageLink("Scheduled Tasks").click();

	const row = page.getByTestId("task-row").filter({ hasText: "file-task" });
	await expect.element(row).toBeVisible();
	await row.hover();
	await row.getByTestId("task-menu-button").click();
	await page.getByRole("menuitem", { name: "Delete" }).click();
	await page.getByRole("button", { name: "Delete", exact: true }).click();

	await expect.poll(() => count(row)).toBe(0);
	await expect
		.element(page.getByText("No scheduled tasks configured for this agent."))
		.toBeVisible();
	expect(savedConfig).toMatchObject({ agents: [{ tasks: [] }] });
});

test("agent files editor opens the file picked in the sidebar", async () => {
	await openAgentPage("files");

	const files = page.getByTestId("sidebar-files").getByTestId("file-row");
	await expect
		.poll(() => texts(files))
		.toEqual(["AGENTS.md", "IDENTITY.md", "MEMORY.md", "RULES.md"]);
	const editor = page.getByPlaceholder(
		"Pick a file in the sidebar, or add one with its +.",
	);
	// Without a ?file the page opens the first file.
	await expect.element(editor).toHaveValue("# Agents");
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("AGENTS.md");
	await expect.element(page.getByTestId("agent-file-builtin")).toBeVisible();

	await files.filter({ hasText: "IDENTITY.md" }).getByRole("link").click();
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/files\?file=IDENTITY\.md$/);
	await expect.element(editor).toHaveValue("# Identity");
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("IDENTITY.md");
	await expect
		.poll(() => count(page.getByTestId("agent-file-builtin")))
		.toBe(0);
});

test("agent files editor auto-syncs templates when an older agent has no root files", async () => {
	const syncedFiles = new Map<string, string>([
		["AGENTS.md", "# Agents"],
		["RULES.md", "# Rules"],
		["MEMORY.md", "Remembered note"],
	]);
	let synced = false;

	mockMCP({
		config_get: CONFIG,
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [ASSISTANT],
		tool_list: TOOLS,
		agent_file_list: () =>
			synced ? Array.from(syncedFiles.keys()).sort() : [],
		agent_file_read: (args?: Args) =>
			syncedFiles.get(String(args?.file ?? "")) ?? "",
		agent_template_sync: () => {
			synced = true;
			return "ok";
		},
	});

	await openAgentPage("files");
	await page
		.getByTestId("agent-file-bar")
		.getByRole("button", { name: "Refresh", exact: true })
		.click();

	await expect
		.poll(() =>
			texts(page.getByTestId("sidebar-files").getByTestId("file-row")),
		)
		.toEqual(["AGENTS.md", "MEMORY.md", "RULES.md"]);
	await expect.poll(() => count(page.getByText("No files yet"))).toBe(0);
});

test("a task can target one of its agent's projects and keeps it when saved", async () => {
	let savedConfig: unknown = null;
	const config = {
		...CONFIG,
		agents: [
			{
				...CONFIG.agents[0],
				fallbacks: ["cli/claude/opus"],
				tasks: [
					...CONFIG.agents[0].tasks,
					{
						name: "nightly-triage",
						type: "project",
						project: "o/gone",
						model: "cli/codex",
						schedule: "0 3 * * *",
						prompt: "Triage new issues",
					},
				],
				// A project task runs against a project its own agent owns.
				projects: [{ target: "o/r" }, { target: "o/other" }],
			},
			{
				name: "elsewhere",
				model: "anthropic/claude-sonnet-4-5",
				projects: [{ target: "o/not-mine" }],
			},
		],
	};
	mockMCP({
		config_get: config,
		config_save: (args?: Args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}"));
			return "ok";
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [ASSISTANT],
		agent_file_list: [],
	});

	await renderApp("/agents/assistant/tasks");
	await openTaskSettings("nightly-triage");

	// A project that is no longer watched stays selected instead of clearing.
	const project = page.getByRole("combobox", { name: "Project", exact: true });
	await expectValue(project, "o/gone");
	expect(await optionLabels(project)).toEqual(["o/r", "o/other", "o/gone"]);
	const model = page.getByTestId("task-model");
	await expect.element(model.getByRole("combobox")).toHaveValue("cli/codex");
	await expect
		.poll(() => count(page.getByPlaceholder("./docs/**/*.md")))
		.toBe(0);

	await chooseOption(project, "o/r");
	await searchOption(model.getByRole("combobox"), "cli/claude/opus");
	await expect
		.element(model.getByRole("combobox"))
		.toHaveValue("cli/claude/opus");

	// Turning the first task into a project task offers the watched projects.
	await openTaskSettings("daily-briefing");
	await chooseOption(
		page.getByRole("combobox", { name: "Task type" }),
		"Project",
	);
	await chooseOption(
		page.getByRole("combobox", { name: "Project", exact: true }),
		"o/other",
	);

	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.element(page.getByTitle("Settings saved")).toBeVisible();
	expect(savedConfig).toMatchObject({
		agents: [
			{
				tasks: [
					{ name: "daily-briefing", type: "project", project: "o/other" },
					{
						name: "nightly-triage",
						type: "project",
						project: "o/r",
						model: "cli/claude/opus",
					},
				],
				projects: [{ target: "o/r" }, { target: "o/other" }],
			},
			{ name: "elsewhere", projects: [{ target: "o/not-mine" }] },
		],
	});
});

test("a task's session target is picked by name and saved by ID", async () => {
	let savedConfig: unknown = null;
	const config = {
		...CONFIG,
		agents: [
			{
				...CONFIG.agents[0],
				tasks: [
					{
						name: "daily-briefing",
						schedule: "0 9 * * *",
						prompt: "Summarize updates",
						target: "session:sess_gone",
					},
				],
			},
		],
	};
	mockMCP({
		config_get: config,
		config_save: (args?: Args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}"));
			return "ok";
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [
			{
				id: "sess_planning",
				agent_id: "assistant",
				name: "sess_planning",
				title: "Trip planning",
				created_at: "",
			},
			{ id: "sess_main", agent_id: "assistant", name: "main", created_at: "" },
		],
		agent_list: [ASSISTANT],
		agent_file_list: [],
	});

	await renderApp("/agents/assistant/tasks");
	await openTaskSettings("daily-briefing");

	// A saved session that no longer exists is shown as gone, not blank.
	const session = page.getByRole("combobox", { name: "Session" });
	expect(await optionLabels(session)).toEqual([
		"Main Chat",
		"Trip planning",
		"Deleted session (no longer exists)",
	]);
	await expectValue(session, "sess_gone");
	await expect
		.element(session)
		.toHaveValue("Deleted session (no longer exists)");
	await expect
		.poll(() => count(page.locator('input[placeholder="Target ID"]')))
		.toBe(0);

	await chooseOption(session, "Main Chat");
	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.element(page.getByTitle("Settings saved")).toBeVisible();
	expect(savedConfig).toMatchObject({
		agents: [{ tasks: [{ target: "session:sess_main" }] }],
	});
});

test("a task saved with a session name selects that session", async () => {
	const config = {
		...CONFIG,
		agents: [
			{
				...CONFIG.agents[0],
				tasks: [
					{
						name: "daily-briefing",
						schedule: "0 9 * * *",
						prompt: "Summarize updates",
						target: "session:main",
					},
				],
			},
		],
	};
	mockMCP({
		config_get: config,
		auth_list: [],
		session_list: [
			{ id: "sess_main", agent_id: "assistant", name: "main", created_at: "" },
		],
		agent_list: [{ id: "a1", name: "assistant", state: "idle" }],
		agent_file_list: [],
	});

	await renderApp("/agents/assistant/tasks");
	await openTaskSettings("daily-briefing");
	const session = page.getByRole("combobox", { name: "Session" });
	await expectValue(session, "sess_main");
	await expect.element(session).toHaveValue("Main Chat");
});

test("tasks use the shared trigger editor and save a daily trigger without cron", async () => {
	let savedConfig: unknown = null;
	const previews: Record<string, unknown>[] = [];
	mockMCP({
		config_get: CONFIG,
		config_save: (args?: Args) => {
			savedConfig = JSON.parse(String(args?.config ?? "{}"));
			return "ok";
		},
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [ASSISTANT],
		agent_file_list: [],
		task_preview: (args?: Record<string, unknown>) => {
			previews.push(args ?? {});
			return {
				description: "preview",
				next: [
					"2026-11-22T17:00:00Z",
					"2026-11-23T17:00:00Z",
					"2026-11-24T17:00:00Z",
				],
			};
		},
	});

	await renderApp("/agents/assistant/tasks?task=daily-briefing");
	const editor = page.getByTestId("trigger-editor");

	// A hand-written cron expression opens on the Cron tab, unchanged.
	await expect
		.element(editor.getByRole("radio", { name: "Cron" }))
		.toHaveAttribute("aria-checked", "true");
	await expect
		.element(editor.getByLabelText("Cron expression"))
		.toHaveValue("0 * * * * *");
	await expect
		.poll(() => count(editor.getByTestId("trigger-preview").locator("li")))
		.toBe(3);

	await editor.getByRole("radio", { name: "Daily" }).click();
	await editor.getByLabelText("At", { exact: true }).fill("09:00");
	await searchOption(editor.getByLabelText("Timezone"), "America/Los_Angeles");
	await chooseOption(
		editor.getByLabelText("If the last run is still going"),
		"Skip this run",
	);
	await expect
		.poll(() => previews.at(-1))
		.toMatchObject({
			trigger: "daily",
			at: "09:00",
			timezone: "America/Los_Angeles",
		});

	await page.getByRole("button", { name: "Save Changes" }).click();
	await expect.element(page.getByTitle("Settings saved")).toBeVisible();
	const task = (
		savedConfig as { agents: { tasks: Record<string, unknown>[] }[] }
	).agents[0].tasks[0];
	expect(task).toMatchObject({
		trigger: "daily",
		at: "09:00",
		timezone: "America/Los_Angeles",
		overlap: "skip",
	});
	expect(task.schedule || undefined).toBeUndefined();
});
