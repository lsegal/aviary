import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// A Code Loop project in manual mode (#1307) is never searched for issues.
// The mocks keep each project's mode the way the Manager does: project_update
// sets it, and project_status reports it in modes, with no issue count for a
// manual project.

const PROJECTS = ["lsegal/aviary", "lsegal/zvidlib"];

type Calls = Record<string, Record<string, unknown>[]>;

async function setup(
	page: Page,
	manual: string[] = [],
	defaultMode: "watch" | "manual" = "watch",
) {
	await setAuthToken(page);
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
	await mockMCP(page, {
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

const board = (page: Page, project: string) =>
	page.goto(`/agents/coder/projects/${encodeURIComponent(project)}`);

test("a manual project's header says so and hides the filter editor", async ({
	page,
}) => {
	await setup(page, ["lsegal/aviary"]);
	await board(page, "lsegal/aviary");
	const subtitle = page.getByTestId("board-subtitle");
	await expect(subtitle).toContainText("Manual: no issue watching");
	await expect(subtitle).toContainText(/checked \d+s ago/);
	await expect(subtitle).not.toContainText("Issues matching");
	await expect(page.getByTestId("project-manual-chip")).toHaveText("Manual");
	await expect(page.getByTestId("issue-filter")).toHaveCount(0);
	await expect(page.getByTestId("issue-filter-edit")).toHaveCount(0);

	// The sidebar shows "manual" in place of a count, only for that project.
	const sidebar = page.getByTestId("sidebar-agent-projects").first();
	await expect(
		sidebar
			.getByRole("link", { name: /lsegal\/aviary/ })
			.getByTestId("project-manual-badge"),
	).toHaveText("manual");
	await expect(
		sidebar
			.getByRole("link", { name: /lsegal\/zvidlib/ })
			.getByTestId("project-manual-badge"),
	).toHaveCount(0);
});

// #1461: the menu offers only Watch and Manual, checks the effective mode,
// and stores an override only for the mode that differs from the global one.
async function expectTwoModes(page: Page) {
	const items = page.getByTestId("project-mode-items");
	await expect(items.getByRole("menuitemradio")).toHaveCount(2);
	await expect(items).not.toContainText(/inherit|overridden/i);
	await expect(items).not.toContainText("Reset to global");
}

test("the header menu switches a project between watch and manual", async ({
	page,
}) => {
	const calls = await setup(page);
	await board(page, "lsegal/zvidlib");
	await expect(page.getByTestId("issue-filter")).toHaveText("assignee:@me");
	await expect(page.getByTestId("project-manual-chip")).toHaveCount(0);

	await page.getByRole("button", { name: "Issue discovery: Watch" }).click();
	await expectTwoModes(page);
	await expect(page.getByTestId("project-mode-watch")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await page.getByTestId("project-mode-manual").click();
	expect(calls.project_update).toEqual([
		{ target: "lsegal/zvidlib", mode: "manual" },
	]);
	await expect(page.getByTestId("project-manual-chip")).toHaveText("Manual");
	await expect(page.getByTestId("board-subtitle")).toContainText(
		"Manual: no issue watching",
	);

	// Watch is the global mode, so picking it clears the override.
	await page
		.getByRole("button", {
			name: "Issue discovery: Manual (no issue watching)",
		})
		.click();
	await expectTwoModes(page);
	await page.getByTestId("project-mode-watch").click();
	expect(calls.project_update.at(-1)).toEqual({
		target: "lsegal/zvidlib",
		mode: "inherit",
	});
	await expect(page.getByTestId("issue-filter")).toHaveText("assignee:@me");
	await expect(page.getByTestId("project-manual-chip")).toHaveCount(0);
});

test("under a global manual mode, watch is the override and manual clears it", async ({
	page,
}) => {
	const calls = await setup(page, [], "manual");
	await board(page, "lsegal/zvidlib");
	await expect(page.getByTestId("project-manual-chip")).toHaveText("Manual");

	await page
		.getByRole("button", {
			name: "Issue discovery: Manual (no issue watching)",
		})
		.click();
	await expectTwoModes(page);
	await expect(page.getByTestId("project-mode-manual")).toHaveAttribute(
		"aria-checked",
		"true",
	);
	await page.getByTestId("project-mode-watch").click();
	expect(calls.project_update).toEqual([
		{ target: "lsegal/zvidlib", mode: "watch" },
	]);
	await expect(page.getByTestId("project-manual-chip")).toHaveCount(0);
	await expect(page.getByTestId("issue-filter")).toHaveText("assignee:@me");

	// The other project still follows the global manual mode.
	const sidebar = page.getByTestId("sidebar-agent-projects").first();
	await expect(
		sidebar
			.getByRole("link", { name: /lsegal\/aviary/ })
			.getByTestId("project-manual-badge"),
	).toHaveText("manual");

	await page.getByRole("button", { name: "Issue discovery: Watch" }).click();
	await page.getByTestId("project-mode-manual").click();
	expect(calls.project_update.at(-1)).toEqual({
		target: "lsegal/zvidlib",
		mode: "inherit",
	});
	await expect(page.getByTestId("project-manual-chip")).toHaveText("Manual");
});

test("the header chip carries the issue counts; there is no Targets/Version footer (#1434)", async ({
	page,
}) => {
	await setup(page, ["lsegal/aviary"]);
	await page.goto("/agents/coder/projects");
	const chip = page.getByTestId("board-issues");
	await expect(chip).toHaveText("4 open issues");
	await expect(chip).toHaveAttribute(
		"title",
		"lsegal/aviary: manual\nlsegal/zvidlib: 4",
	);
	await expect(page.getByText(/Targets:/)).toHaveCount(0);
	await expect(page.getByText(/^Version /)).toHaveCount(0);

	await board(page, "lsegal/zvidlib");
	await expect(page.getByTestId("board-issues")).toHaveText("4 open issues");
	await expect(page.getByText(/Targets:/)).toHaveCount(0);

	// A manual project has its Manual chip and no count.
	await board(page, "lsegal/aviary");
	await expect(page.getByTestId("project-manual-chip")).toHaveText("Manual");
	await expect(page.getByTestId("board-issues")).toHaveCount(0);
});
