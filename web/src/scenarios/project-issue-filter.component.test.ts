import { describe, expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { goto, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP } from "../component-test/server";
import { openBoardInfo } from "./helpers/boardHeader";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/ProjectsView.vue"));

// The Code Loop header's issue filter is editable inline for one project
// (#1213). The mocks keep each project's own filter the way the Manager does:
// project_update sets or resets one project's, and project_status reports
// every project's effective filter plus which ones are custom.

const SHARED = "assignee:@me";
const PROJECTS = ["lsegal/aviary", "lsegal/zvidlib"];

type Calls = Record<string, Record<string, unknown>[]>;

function setup() {
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

const boardURL = (project: string) =>
	`/agents/coder/projects/${encodeURIComponent(project)}`;

const count = (loc: Locator) => () => loc.elements().length;

/** Focuses the element and presses key on it, as Playwright's locator.press. */
async function press(loc: Locator, key: string) {
	(loc.element() as HTMLElement).focus();
	await commands.keyPress(key);
}

/** A touch tap, as Playwright's locator.tap with hasTouch. */
async function tap(loc: Locator) {
	await expect.element(loc).toBeVisible();
	const el = loc.element() as HTMLElement;
	const r = el.getBoundingClientRect();
	const init: PointerEventInit = {
		bubbles: true,
		cancelable: true,
		composed: true,
		pointerType: "touch",
		isPrimary: true,
		pointerId: 1,
		clientX: r.left + r.width / 2,
		clientY: r.top + r.height / 2,
	};
	el.dispatchEvent(new PointerEvent("pointerdown", init));
	el.dispatchEvent(new PointerEvent("pointerup", init));
	el.click();
}

test("edit → save sets the project's own filter only", async () => {
	const calls = setup();
	await renderApp(boardURL("lsegal/aviary"));
	await openBoardInfo();
	const subtitle = page.getByTestId("board-subtitle");
	await expect.element(page.getByTestId("issue-filter")).toHaveText(SHARED);
	await expect.poll(count(page.getByTestId("issue-filter-custom"))).toBe(0);

	const pencil = page.getByRole("button", {
		name: "Edit issue filter for this project",
	});
	await expect
		.element(pencil)
		.toHaveAttribute("title", "Edit issue filter for this project");
	await pencil.click();
	const input = page.getByRole("textbox", {
		name: "Issue filter for lsegal/aviary",
	});
	await expect.element(input).toHaveFocus();
	await expect.element(input).toHaveValue(SHARED);
	await expect.element(input).toHaveCSS("font-family", /mono/i);
	// The dry run reports the match count while editing, announced politely.
	const status = page.getByTestId("issue-filter-status");
	await expect.element(status).toHaveAttribute("role", "status");
	await expect.element(status).toHaveText("Matches 12 open issues");
	await expect.poll(count(page.getByTestId("issue-filter-reset"))).toBe(0);

	await input.fill("label:bug  -label:wip");
	await expect.element(status).toHaveText("Matches 3 open issues");
	await press(input, "Enter");
	await expect.poll(count(page.getByTestId("issue-filter-editor"))).toBe(0);
	expect(calls.project_update).toEqual([
		{ target: "lsegal/aviary", filter: "label:bug -label:wip" },
	]);
	// Saving refreshed the project at once.
	expect(calls.project_refresh).toEqual([{ target: "lsegal/aviary" }]);
	await expect
		.element(page.getByTestId("issue-filter"))
		.toHaveText("label:bug -label:wip");
	await expect
		.element(page.getByTestId("issue-filter-custom"))
		.toHaveText("custom");
	await expect.element(subtitle).toMatchTextContent("browser");

	// The other project keeps the shared filter.
	await goto(boardURL("lsegal/zvidlib"));
	await openBoardInfo();
	await expect.element(page.getByTestId("issue-filter")).toHaveText(SHARED);
	await expect.poll(count(page.getByTestId("issue-filter-custom"))).toBe(0);
});

test("reset to default removes the project's own filter", async () => {
	const calls = setup();
	await renderApp(boardURL("lsegal/aviary"));
	await openBoardInfo();
	await page.getByTestId("issue-filter").dblClick();
	await page.getByTestId("issue-filter-input").fill("label:bug");
	await page.getByTestId("issue-filter-save").click();
	await expect.element(page.getByTestId("issue-filter-custom")).toBeVisible();

	await page.getByTestId("issue-filter-edit").click();
	await page.getByTestId("issue-filter-reset").click();
	await expect.poll(count(page.getByTestId("issue-filter-editor"))).toBe(0);
	expect(calls.project_update.at(-1)).toEqual({
		target: "lsegal/aviary",
		reset_filter: true,
	});
	await expect.element(page.getByTestId("issue-filter")).toHaveText(SHARED);
	await expect.poll(count(page.getByTestId("issue-filter-custom"))).toBe(0);
});

test("an invalid filter is rejected inline and never saved", async () => {
	const calls = setup();
	await renderApp(boardURL("lsegal/aviary"));
	await openBoardInfo();
	await page.getByTestId("issue-filter-edit").click();
	const input = page.getByTestId("issue-filter-input");
	const status = page.getByTestId("issue-filter-status");

	await input.fill("label:");
	await expect
		.element(status)
		.toHaveText("Invalid filter: label: needs a value");
	await expect.element(input).toHaveAttribute("aria-invalid", "true");
	await expect.element(page.getByTestId("issue-filter-save")).toBeDisabled();
	await press(input, "Enter");

	await input.fill("assignee:nosuchuser");
	await expect
		.element(status)
		.toMatchTextContent("The listed users cannot be searched");
	await expect.element(page.getByTestId("issue-filter-save")).toBeDisabled();
	expect(calls.project_update).toBeUndefined();

	// Esc cancels and leaves the filter as it was.
	await press(input, "Escape");
	await expect.poll(count(page.getByTestId("issue-filter-editor"))).toBe(0);
	await expect.element(page.getByTestId("issue-filter")).toHaveText(SHARED);
	expect(calls.project_update).toBeUndefined();
});

test("the filter editor works by keyboard alone", async () => {
	const calls = setup();
	await renderApp(boardURL("lsegal/aviary"));
	await openBoardInfo();
	await press(page.getByTestId("issue-filter-edit"), "Enter");
	const input = page.getByTestId("issue-filter-input");
	await expect.element(input).toHaveFocus();
	await commands.keyPress("Tab");
	await expect.element(page.getByTestId("issue-filter-save")).toHaveFocus();
	await commands.keyPress("Tab");
	await expect.element(page.getByTestId("issue-filter-cancel")).toHaveFocus();
	await commands.keyPress("Enter");
	await expect.poll(count(page.getByTestId("issue-filter-editor"))).toBe(0);
	expect(calls.project_update).toBeUndefined();
});

describe("on a phone", () => {
	test("tapping the pencil opens an editor that fits the screen", async () => {
		await page.viewport(375, 740);
		setup();
		await renderApp(boardURL("lsegal/aviary"));
		await openBoardInfo(true);
		await tap(page.getByTestId("issue-filter-edit"));
		const editor = page.getByTestId("issue-filter-editor");
		await expect.element(editor).toBeVisible();
		await page.getByTestId("issue-filter-input").fill("label:bug -label:wip");
		await expect
			.element(page.getByTestId("issue-filter-status"))
			.toHaveText("Matches 3 open issues");
		for (const id of [
			"issue-filter-input",
			"issue-filter-save",
			"issue-filter-cancel",
		]) {
			const box = page.getByTestId(id).element().getBoundingClientRect();
			expect(box.width, id).toBeGreaterThan(0);
			expect(box.x + box.width, id).toBeLessThanOrEqual(375);
		}
		const overflow =
			document.documentElement.scrollWidth -
			document.documentElement.clientWidth;
		expect(overflow).toBeLessThanOrEqual(0);
	});
});
