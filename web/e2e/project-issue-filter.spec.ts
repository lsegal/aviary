import { expect, type Page, test } from "@playwright/test";
import { openBoardInfo } from "./helpers/boardHeader";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// The Code Loop header's issue filter is editable inline for one project
// (#1213). The mocks keep each project's own filter the way the Manager does:
// project_update sets or resets one project's, and project_status reports
// every project's effective filter plus which ones are custom.

const SHARED = "assignee:@me";
const PROJECTS = ["lsegal/aviary", "lsegal/zvidlib"];

type Calls = Record<string, Record<string, unknown>[]>;

async function setup(page: Page) {
	await setAuthToken(page);
	const calls: Calls = {};
	const record = (tool: string, args?: Record<string, unknown>) => {
		calls[tool] ??= [];
		calls[tool].push(args ?? {});
	};
	const own: Record<string, string> = {};
	const validate = (filter: string) => {
		if (/(^|\s)-?\w+:(\s|$)/.test(`${filter} `))
			throw new Error("invalid filter: label: needs a value");
		if (filter.includes("nosuchuser"))
			throw new Error(
				"invalid filter: The listed users cannot be searched either because the users do not exist or you do not have permission to view the users.",
			);
		return filter.includes("label:bug") ? 3 : 12;
	};
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
		project_status: (args?: Record<string, unknown>) => {
			record("project_status", args);
			return {
				identity: "FC88FDCA",
				running: true,
				targets: PROJECTS,
				issue_counts: {},
				jobs: {},
				states: {},
				delivery: "browser",
				poll_interval: "20s",
				issue_filter: SHARED,
				issue_filters: Object.fromEntries(
					PROJECTS.map((p) => [p, own[p] ?? SHARED]),
				),
				custom_filters: PROJECTS.filter((p) => own[p]),
				clis: [],
				recent_logs: "",
			};
		},
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
		project_task_list: { tasks: [] },
		project_filter_check: (args?: Record<string, unknown>) => {
			record("project_filter_check", args);
			const filter = String(args?.filter ?? "");
			return { target: args?.target, filter, matches: validate(filter) };
		},
		project_update: (args?: Record<string, unknown>) => {
			record("project_update", args);
			const target = String(args?.target);
			if (args?.reset_filter) delete own[target];
			else {
				validate(String(args?.filter));
				own[target] = String(args?.filter);
			}
			record("project_refresh", { target });
			return { target, custom: !!own[target] };
		},
	});
	return calls;
}

const board = (page: Page, project: string) =>
	page.goto(`/agents/coder/projects/${encodeURIComponent(project)}`);

test("edit → save sets the project's own filter only", async ({ page }) => {
	const calls = await setup(page);
	await board(page, "lsegal/aviary");
	await openBoardInfo(page);
	const subtitle = page.getByTestId("board-subtitle");
	await expect(page.getByTestId("issue-filter")).toHaveText(SHARED);
	await expect(page.getByTestId("issue-filter-custom")).toHaveCount(0);

	const pencil = page.getByRole("button", {
		name: "Edit issue filter for this project",
	});
	await expect(pencil).toHaveAttribute(
		"title",
		"Edit issue filter for this project",
	);
	await pencil.click();
	const input = page.getByRole("textbox", {
		name: "Issue filter for lsegal/aviary",
	});
	await expect(input).toBeFocused();
	await expect(input).toHaveValue(SHARED);
	await expect(input).toHaveCSS("font-family", /mono/i);
	// The dry run reports the match count while editing, announced politely.
	const status = page.getByTestId("issue-filter-status");
	await expect(status).toHaveAttribute("role", "status");
	await expect(status).toHaveText("Matches 12 open issues");
	await expect(page.getByTestId("issue-filter-reset")).toHaveCount(0);

	await input.fill("label:bug  -label:wip");
	await expect(status).toHaveText("Matches 3 open issues");
	await input.press("Enter");
	await expect(page.getByTestId("issue-filter-editor")).toHaveCount(0);
	expect(calls.project_update).toEqual([
		{ target: "lsegal/aviary", filter: "label:bug -label:wip" },
	]);
	// Saving refreshed the project at once.
	expect(calls.project_refresh).toEqual([{ target: "lsegal/aviary" }]);
	await expect(page.getByTestId("issue-filter")).toHaveText(
		"label:bug -label:wip",
	);
	await expect(page.getByTestId("issue-filter-custom")).toHaveText("custom");
	await expect(subtitle).toContainText("browser");

	// The other project keeps the shared filter.
	await board(page, "lsegal/zvidlib");
	await openBoardInfo(page);
	await expect(page.getByTestId("issue-filter")).toHaveText(SHARED);
	await expect(page.getByTestId("issue-filter-custom")).toHaveCount(0);
});

test("reset to default removes the project's own filter", async ({ page }) => {
	const calls = await setup(page);
	await board(page, "lsegal/aviary");
	await openBoardInfo(page);
	await page.getByTestId("issue-filter").dblclick();
	await page.getByTestId("issue-filter-input").fill("label:bug");
	await page.getByTestId("issue-filter-save").click();
	await expect(page.getByTestId("issue-filter-custom")).toBeVisible();

	await page.getByTestId("issue-filter-edit").click();
	await page.getByTestId("issue-filter-reset").click();
	await expect(page.getByTestId("issue-filter-editor")).toHaveCount(0);
	expect(calls.project_update.at(-1)).toEqual({
		target: "lsegal/aviary",
		reset_filter: true,
	});
	await expect(page.getByTestId("issue-filter")).toHaveText(SHARED);
	await expect(page.getByTestId("issue-filter-custom")).toHaveCount(0);
});

test("an invalid filter is rejected inline and never saved", async ({
	page,
}) => {
	const calls = await setup(page);
	await board(page, "lsegal/aviary");
	await openBoardInfo(page);
	await page.getByTestId("issue-filter-edit").click();
	const input = page.getByTestId("issue-filter-input");
	const status = page.getByTestId("issue-filter-status");

	await input.fill("label:");
	await expect(status).toHaveText("Invalid filter: label: needs a value");
	await expect(input).toHaveAttribute("aria-invalid", "true");
	await expect(page.getByTestId("issue-filter-save")).toBeDisabled();
	await input.press("Enter");

	await input.fill("assignee:nosuchuser");
	await expect(status).toContainText("The listed users cannot be searched");
	await expect(page.getByTestId("issue-filter-save")).toBeDisabled();
	expect(calls.project_update).toBeUndefined();

	// Esc cancels and leaves the filter as it was.
	await input.press("Escape");
	await expect(page.getByTestId("issue-filter-editor")).toHaveCount(0);
	await expect(page.getByTestId("issue-filter")).toHaveText(SHARED);
	expect(calls.project_update).toBeUndefined();
});

test("the filter editor works by keyboard alone", async ({ page }) => {
	const calls = await setup(page);
	await board(page, "lsegal/aviary");
	await openBoardInfo(page);
	await page.getByTestId("issue-filter-edit").focus();
	await page.keyboard.press("Enter");
	const input = page.getByTestId("issue-filter-input");
	await expect(input).toBeFocused();
	await page.keyboard.press("Tab");
	await expect(page.getByTestId("issue-filter-save")).toBeFocused();
	await page.keyboard.press("Tab");
	await expect(page.getByTestId("issue-filter-cancel")).toBeFocused();
	await page.keyboard.press("Enter");
	await expect(page.getByTestId("issue-filter-editor")).toHaveCount(0);
	expect(calls.project_update).toBeUndefined();
});

test.describe("on a phone", () => {
	test.use({ viewport: { width: 375, height: 740 }, hasTouch: true });

	test("tapping the pencil opens an editor that fits the screen", async ({
		page,
	}) => {
		await setup(page);
		await board(page, "lsegal/aviary");
		await openBoardInfo(page, true);
		await page.getByTestId("issue-filter-edit").tap();
		const editor = page.getByTestId("issue-filter-editor");
		await expect(editor).toBeVisible();
		await page.getByTestId("issue-filter-input").fill("label:bug -label:wip");
		await expect(page.getByTestId("issue-filter-status")).toHaveText(
			"Matches 3 open issues",
		);
		for (const id of [
			"issue-filter-input",
			"issue-filter-save",
			"issue-filter-cancel",
		]) {
			const box = await page.getByTestId(id).boundingBox();
			expect(box, id).not.toBeNull();
			expect((box?.x ?? 0) + (box?.width ?? 0), id).toBeLessThanOrEqual(375);
		}
		const overflow = await page.evaluate(
			() =>
				document.documentElement.scrollWidth -
				document.documentElement.clientWidth,
		);
		expect(overflow).toBeLessThanOrEqual(0);
	});
});
