import { expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { goto, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";
import { openBoardInfo } from "./helpers/boardHeader";
// The pages this file renders (see component-test/routes.ts).
import "../views/ProjectsView.vue";
import "../views/SettingsView.vue";

// A Code Loop project in manual mode (#1307) is never searched for issues.
// The mocks keep each project's mode the way the Manager does: project_update
// sets it, and project_status reports it in modes, with no issue count for a
// manual project.

const PROJECTS = ["lsegal/aviary", "lsegal/zvidlib"];

type Calls = Record<string, Record<string, unknown>[]>;

function setup(
	manual: string[] = [],
	defaultMode: "watch" | "manual" = "watch",
) {
	const calls: Calls = {};
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	// Projects named in manual carry their own manual override; the rest
	// follow defaultMode, the global one (#1371).
	const overrides: Record<string, string> = Object.fromEntries(
		manual.map((p) => [p, "manual"]),
	);
	const modeOf = (p: string) => overrides[p] ?? defaultMode;
	const modes = () => Object.fromEntries(PROJECTS.map((p) => [p, modeOf(p)]));
	mockMCP({
		agent_list: [],
		config_get: {
			agents: [
				{
					name: "coder",
					model: "cli/claude/opus",
					projects: PROJECTS.map((target) => ({ target })),
				},
			],
		},
		project_status: () => ({
			identity: "FC88FDCA",
			running: true,
			targets: PROJECTS,
			issue_counts: Object.fromEntries(
				PROJECTS.filter((p) => modeOf(p) === "watch").map((p) => [p, 4]),
			),
			modes: modes(),
			mode_sources: Object.fromEntries(
				PROJECTS.map((p) => [p, overrides[p] ? "project" : "global"]),
			),
			default_mode: defaultMode,
			jobs: {},
			states: {},
			delivery: "browser",
			poll_interval: "20s",
			last_check: new Date(Date.now() - 5000).toISOString(),
			issue_filter: "assignee:@me",
			issue_filters: Object.fromEntries(
				PROJECTS.map((p) => [p, "assignee:@me"]),
			),
			custom_filters: [],
			clis: [],
			recent_logs: "",
		}),
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
		project_task_list: { tasks: [] },
		project_update: (args?: Record<string, unknown>) => {
			record("project_update", args);
			const target = String(args?.target);
			if (args?.mode === "inherit") delete overrides[target];
			else overrides[target] = String(args?.mode);
			return { target, mode: modeOf(target) };
		},
	});
	return calls;
}

const boardURL = (project: string) =>
	`/agents/coder/projects/${encodeURIComponent(project)}`;

async function expectCount(loc: Locator, n: number) {
	await expect.poll(() => loc.elements().length).toBe(n);
}

test("a manual project's header says so and hides the filter editor", async () => {
	setup(["lsegal/aviary"]);
	await renderApp(boardURL("lsegal/aviary"));
	const subtitle = (await openBoardInfo()).getByTestId("board-subtitle");
	await expect
		.element(subtitle)
		.toMatchTextContent("Manual: no issue watching");
	await expect.element(page.getByTestId("board-mode")).toHaveText("Manual");
	await expect
		.element(page.getByTestId("board-checked"))
		.toHaveText(/\d+s ago/);
	await expect.element(subtitle).not.toMatchTextContent("Issues matching");
	await expect
		.element(page.getByTestId("project-manual-chip"))
		.toHaveText("Manual");
	await expectCount(page.getByTestId("issue-filter"), 0);
	await expectCount(page.getByTestId("issue-filter-edit"), 0);

	// The sidebar shows "manual" in place of a count, only for that project.
	const sidebar = page.getByTestId("sidebar-agent-projects").first();
	await expect
		.element(
			sidebar
				.getByRole("link", { name: /lsegal\/aviary/ })
				.getByTestId("project-manual-badge"),
		)
		.toHaveText("manual");
	await expectCount(
		sidebar
			.getByRole("link", { name: /lsegal\/zvidlib/ })
			.getByTestId("project-manual-badge"),
		0,
	);
});

// #1461: the menu offers only Watch and Manual, checks the effective mode,
// and stores an override only for the mode that differs from the global one.
async function expectTwoModes() {
	const items = page.getByTestId("project-mode-items");
	await expectCount(items.getByRole("menuitemradio"), 2);
	await expect.element(items).not.toMatchTextContent(/inherit|overridden/i);
	await expect.element(items).not.toMatchTextContent("Reset to global");
}

test("the header menu switches a project between watch and manual", async () => {
	const calls = setup();
	await renderApp(boardURL("lsegal/zvidlib"));
	await openBoardInfo();
	await expect
		.element(page.getByTestId("issue-filter"))
		.toHaveText("assignee:@me");
	await expect.element(page.getByTestId("board-mode")).toHaveText("Watch");
	await commands.keyPress("Escape");
	await expectCount(page.getByTestId("project-manual-chip"), 0);

	await page.getByRole("button", { name: "Issue discovery: Watch" }).click();
	await expectTwoModes();
	await expect
		.element(page.getByTestId("project-mode-watch"))
		.toHaveAttribute("aria-checked", "true");
	await page.getByTestId("project-mode-manual").click();
	expect(calls.project_update).toEqual([
		{ target: "lsegal/zvidlib", mode: "manual" },
	]);
	await expect
		.element(page.getByTestId("project-manual-chip"))
		.toHaveText("Manual");
	await expect
		.element((await openBoardInfo()).getByTestId("board-subtitle"))
		.toMatchTextContent("Manual: no issue watching");
	await commands.keyPress("Escape");

	// Watch is the global mode, so picking it clears the override.
	await page
		.getByRole("button", {
			name: "Issue discovery: Manual (no issue watching)",
		})
		.click();
	await expectTwoModes();
	await page.getByTestId("project-mode-watch").click();
	expect(calls.project_update.at(-1)).toEqual({
		target: "lsegal/zvidlib",
		mode: "inherit",
	});
	await openBoardInfo();
	await expect
		.element(page.getByTestId("issue-filter"))
		.toHaveText("assignee:@me");
	await expectCount(page.getByTestId("project-manual-chip"), 0);
});

test("under a global manual mode, watch is the override and manual clears it", async () => {
	const calls = setup([], "manual");
	await renderApp(boardURL("lsegal/zvidlib"));
	await expect
		.element(page.getByTestId("project-manual-chip"))
		.toHaveText("Manual");

	await page
		.getByRole("button", {
			name: "Issue discovery: Manual (no issue watching)",
		})
		.click();
	await expectTwoModes();
	await expect
		.element(page.getByTestId("project-mode-manual"))
		.toHaveAttribute("aria-checked", "true");
	await page.getByTestId("project-mode-watch").click();
	expect(calls.project_update).toEqual([
		{ target: "lsegal/zvidlib", mode: "watch" },
	]);
	await expectCount(page.getByTestId("project-manual-chip"), 0);
	await openBoardInfo();
	await expect
		.element(page.getByTestId("issue-filter"))
		.toHaveText("assignee:@me");
	await commands.keyPress("Escape");

	// The other project still follows the global manual mode.
	const sidebar = page.getByTestId("sidebar-agent-projects").first();
	await expect
		.element(
			sidebar
				.getByRole("link", { name: /lsegal\/aviary/ })
				.getByTestId("project-manual-badge"),
		)
		.toHaveText("manual");

	await page.getByRole("button", { name: "Issue discovery: Watch" }).click();
	await page.getByTestId("project-mode-manual").click();
	expect(calls.project_update.at(-1)).toEqual({
		target: "lsegal/zvidlib",
		mode: "inherit",
	});
	await expect
		.element(page.getByTestId("project-manual-chip"))
		.toHaveText("Manual");
});

test("the header chip carries the issue counts; there is no Targets/Version footer (#1434)", async () => {
	setup(["lsegal/aviary"]);
	await renderApp("/agents/coder/projects");
	const chip = page.getByTestId("board-issues");
	await expect.element(chip).toHaveText("4 open issues");
	await expect
		.element(chip)
		.toHaveAttribute("title", "lsegal/aviary: manual\nlsegal/zvidlib: 4");
	await expectCount(page.getByText(/Targets:/), 0);
	await expectCount(page.getByText(/^Version /), 0);

	await goto(boardURL("lsegal/zvidlib"));
	await expect
		.element(page.getByTestId("board-issues"))
		.toHaveText("4 open issues");
	await expectCount(page.getByText(/Targets:/), 0);

	// A manual project has its Manual chip and no count.
	await goto(boardURL("lsegal/aviary"));
	await expect
		.element(page.getByTestId("project-manual-chip"))
		.toHaveText("Manual");
	await expectCount(page.getByTestId("board-issues"), 0);
});
