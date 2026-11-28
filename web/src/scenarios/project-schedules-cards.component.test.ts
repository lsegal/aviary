// Ported from web/e2e/project-schedules.spec.ts (#1636): a Code Loop board's
// scheduled cards, their order, actions and kanban boxes.
import { afterEach, expect, test, vi } from "vitest";
import { commands, page } from "vitest/browser";
import { currentURL, goto, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { clickCardAt } from "./helpers/cards";
import {
	BOARD,
	closeTaskDialog,
	expectCount,
	expectHidden,
	expectTexts,
	FIRE,
	ISSUE_JOB,
	minutesFromNow,
	schedule,
	setup,
	shown,
} from "./project-schedules-fixtures";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../components/projects/ProjectJobDetail.vue"),
	() => import("../views/ProjectsView.vue"),
	() => import("../views/TaskView.vue"),
);

afterEach(() => {
	vi.useRealTimers();
});

test("board lists scheduled tasks as their own boxes with run details", async () => {
	setup();
	await renderApp(BOARD);

	const boxes = page.getByTestId("scheduled-card");
	// Only this project's scheduled tasks, by their next run.
	await expectCount(boxes, 2);
	const nightly = boxes.nth(0);
	await expect
		.element(nightly.getByRole("heading"))
		.toHaveText("nightly-triage");
	await expect
		.element(nightly.getByTestId("scheduled-trigger"))
		.toHaveText("daily at 02:00 (America/Toronto)");
	await expect
		.element(nightly.getByTestId("scheduled-next"))
		.toMatchTextContent(/in 4[12]m/);
	// The output area shows the run's output, not the last finished result.
	await expectCount(nightly.getByTestId("scheduled-last"), 0);
	await expect
		.element(nightly.getByTestId("scheduled-agent"))
		.toHaveText("assistant · cli/claude/opus");
	await expect
		.element(boxes.nth(1).getByTestId("scheduled-next"))
		.toHaveText("Next run: paused");
	await expect.element(boxes.nth(1).getByTestId("paused-chip")).toBeVisible();
	await expect
		.element(boxes.nth(1).getByTestId("scheduled-last"))
		.toHaveText("Hasn't run yet");

	// A recurring schedule's run in flight shows on its own card, as its
	// state and the control that opens the run, beside its next fire time
	// and its last finished result; no job card copies it (#977).
	const running = nightly.getByTestId("scheduled-running");
	await expect.element(running).toHaveText(/^\s*Running · [12]m\s*$/);
	await expect
		.element(nightly.getByTestId("scheduled-state"))
		.toHaveText("Running");
	await expectCount(page.getByTestId("job-card"), 0);
	// The gear to its task settings stays beside it.
	await expect
		.element(nightly.getByRole("button", { name: "Task settings" }))
		.toBeVisible();
	// It shows the run's output like an issue card does.
	await expect
		.element(nightly.getByTestId("scheduled-output"))
		.toHaveText("Reading the issues opened since 02:00 yesterday…");
	await expect.element(nightly.getByTestId("output-mode")).toBeVisible();
	await running.click();
	await expect.poll(currentURL).toMatch(new RegExp(`/jobs/${FIRE}`));
	await goto(BOARD);
	await expectCount(boxes, 2);
	// A schedule with no run has an empty output area, no running control
	// and the gear to its task settings.
	const weekly = boxes.nth(1);
	await expectCount(weekly.getByTestId("scheduled-running"), 0);
	await expectCount(weekly.getByTestId("scheduled-output"), 0);
	await expect.element(weekly.getByTestId("scheduled-empty")).toBeVisible();
	await expectCount(weekly.getByTestId("output-mode"), 0);
	await expect
		.element(weekly.getByRole("button", { name: "Task settings" }))
		.toBeVisible();

	// The search narrows scheduled boxes with the jobs. A state filter shows
	// only the schedules in that state: the running one under Running, and
	// none under Done (#977).
	await page.getByLabelText("Search jobs").fill("dependencies");
	await expectCount(boxes, 1);
	await expect.element(boxes.first()).toMatchTextContent("weekly-deps");
	await page.getByLabelText("Search jobs").fill("");
	const filter = page.getByRole("group", { name: "Job filter" });
	await filter.getByRole("button", { name: /^Running/ }).click();
	await expectCount(boxes, 1);
	await expect.element(boxes.first()).toMatchTextContent("nightly-triage");
	await filter.getByRole("button", { name: /^Running/ }).click();
	await filter.getByRole("button", { name: /^Done/ }).click();
	await expectCount(boxes, 0);
});

test("scheduled cards sit behind running jobs unless their next run is within five minutes (#1207)", async () => {
	const at = (name: string, minutes: number, fields = {}) =>
		schedule({
			id: `assistant/${name}`,
			name,
			prompt: `Run ${name}.`,
			enabled: true,
			every: "1d",
			next_run: minutesFromNow(minutes),
			...fields,
		});
	const issue = (number: number) => ({
		...ISSUE_JOB,
		key: `lsegal/aviary#${number}`,
		number,
		title: `Issue ${number}`,
	});
	// As page.clock.install: the page's clock, which the test moves on.
	vi.useFakeTimers({
		toFake: ["Date", "setInterval", "clearInterval"],
		shouldAdvanceTime: true,
	});
	setup({
		// Listed by name; a running task sorts by its next run, never by when
		// its run started.
		tasks: [
			at("a-two-hours", 120),
			at("b-running", 50, {
				history: [
					{
						fire_time: minutesFromNow(-1),
						reason: "schedule",
						status: "running",
					},
				],
			}),
			at("c-paused", 1, { enabled: false }),
			at("d-twenty", 20),
			at("e-three", 3),
		],
		jobs: [issue(11), issue(12)],
		entries: [],
	});
	await renderApp(BOARD);

	const order = page
		.getByRole("region", { name: "Jobs" })
		.locator("article h3");
	await expectTexts(order, [
		"e-three",
		"#11 Issue 11",
		"#12 Issue 12",
		"d-twenty",
		"b-running",
		"a-two-hours",
		"c-paused",
	]);

	// Sixteen minutes on, the twenty-minute card is due within five minutes
	// and moves up behind the overdue one, with no refresh of the board.
	await vi.advanceTimersByTimeAsync(16 * 60_000);
	await expectTexts(order, [
		"e-three",
		"d-twenty",
		"#11 Issue 11",
		"#12 Issue 12",
		"b-running",
		"a-two-hours",
		"c-paused",
	]);
});

test("a scheduled card's footer runs, pauses and opens settings like a job card's (#948)", async () => {
	const calls = setup();
	await renderApp(BOARD);
	const weekly = page.getByTestId("scheduled-card").nth(1);
	await expect
		.element(weekly.getByTestId("scheduled-agent"))
		.toHaveText("assistant");

	await weekly.getByRole("button", { name: "Run now" }).click();
	await expect.poll(() => calls.task_run?.length ?? 0).toBe(1);
	expect(calls.task_run[0]).toMatchObject({ name: "assistant/weekly-deps" });

	// Paused, it offers Resume; resuming saves it enabled.
	await expectCount(weekly.getByRole("button", { name: "Pause" }), 0);
	await weekly.getByRole("button", { name: "Resume" }).click();
	await expect.poll(() => calls.task_update?.length ?? 0).toBe(1);
	expect(calls.task_update[0]).toMatchObject({
		name: "assistant/weekly-deps",
		enabled: true,
	});
	const nightly = page.getByTestId("scheduled-card").first();
	await nightly.getByRole("button", { name: "Pause" }).click();
	await expect.poll(() => calls.task_update?.length ?? 0).toBe(2);
	expect(calls.task_update[1]).toMatchObject({
		name: "assistant/nightly-triage",
		enabled: false,
	});

	// The gear opens the shared task dialog, as the sidebar's gear does.
	await weekly.getByRole("button", { name: "Task settings" }).click();
	await expect.element(page.getByRole("dialog")).toBeVisible();
	await closeTaskDialog();
});

test("a scheduled card opens its task's page, like its sidebar row (#950)", async () => {
	setup();
	await renderApp(BOARD);
	const weekly = page.getByTestId("scheduled-card").nth(1);
	await expect.element(weekly).toMatchTextContent("weekly-deps");

	// The name is a real link to the task's page, stretched over the card.
	const link = weekly.getByTestId("scheduled-card-link");
	await expect
		.element(link)
		.toHaveAttribute("href", "/agents/assistant/tasks/weekly-deps");
	await clickCardAt(weekly, weekly.getByTestId("scheduled-trigger"));
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\/weekly-deps$/);

	// Kanban cards follow the same rules: card → task page, gear → settings.
	await goto(BOARD);
	await page.getByTestId("kanban-toggle").click();
	const kanban = page
		.getByTestId("kanban-scheduled-card")
		.filter({ hasText: "weekly-deps" });
	await kanban.getByRole("button", { name: "Task settings" }).click();
	await expect
		.element(page.getByRole("dialog", { name: "Edit task" }))
		.toBeVisible();
	await commands.keyPress("Escape");
	await expectCount(page.getByRole("dialog"), 0);
	await expect.poll(currentURL).toMatch(/\/system\/watches\/lsegal%2Faviary/);
	await clickCardAt(kanban, kanban.getByTestId("kanban-scheduled-prompt"));
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\/weekly-deps$/);
});

test("kanban shows a Scheduled column of compact scheduled boxes", async () => {
	setup();
	await renderApp(BOARD);
	await page.getByTestId("kanban-toggle").click();

	const columns = page.getByTestId("kanban-column");
	await expect
		.element(columns.first())
		.toHaveAttribute("data-state", "scheduled");
	const cards = columns.first().getByTestId("kanban-scheduled-card");
	await expectCount(cards, 2);
	await expect
		.element(cards.first().getByTestId("kanban-scheduled-next"))
		.toHaveText(/^Next in 4[12]m$/);
	await expect
		.element(cards.first().getByTestId("kanban-scheduled-prompt"))
		.toMatchTextContent("Triage issues opened since the last run");
	await expect
		.element(cards.nth(1).getByTestId("kanban-scheduled-next"))
		.toHaveText("Paused");
	// The compact card has no output.
	await expectCount(cards.first().locator("pre"), 0);

	// A state filter narrows the board to that state's column.
	await page
		.getByRole("group", { name: "Job filter" })
		.getByRole("button", { name: /^Running/ })
		.click();
	await expectCount(columns, 1);
	await expect
		.element(columns.first())
		.toHaveAttribute("data-state", "running");
});

test("scheduled cards have no pencil; the gear is the last action and opens the full task settings (#1165)", async () => {
	setup();
	await renderApp(BOARD);
	const cards = page.getByTestId("scheduled-card");
	await expectCount(cards, 2);
	for (const card of cards.all()) {
		// No pencil: nothing on the card edits the task but the gear.
		await expectCount(card.getByRole("link", { name: /edit/i }), 0);
		await expectCount(card.getByRole("button", { name: /edit/i }), 0);
		// Run now and Pause or Resume come first; the gear is the last action.
		const actions = [
			...card
				.getByTestId("scheduled-footer")
				.element()
				.querySelectorAll("a, button"),
		].filter(shown);
		const names = actions.map((e) => e.getAttribute("aria-label") ?? "");
		expect(names.at(-1)).toBe("Task settings");
		expect(names.indexOf("Run now")).toBeLessThan(names.length - 1);
		expect(
			Math.max(names.indexOf("Pause"), names.indexOf("Resume")),
		).toBeLessThan(names.length - 1);
		const gear = actions.at(-1) as HTMLElement;
		await expect.element(gear).toHaveAttribute("title", "Task settings");
	}

	// The gear is reachable by keyboard and opens the full task settings,
	// with the newer task options such as its type, target and Send To.
	const gear = cards.first().getByRole("button", { name: "Task settings" });
	(gear.element() as HTMLElement).focus();
	await expect.element(gear).toHaveFocus();
	await commands.keyPress("Enter");
	const dialog = page.getByRole("dialog", { name: "Edit task" });
	await expect.element(dialog).toBeVisible();
	await expect.element(dialog.getByTestId("task-send-to")).toBeVisible();
	await expect
		.element(dialog.getByLabelText("Task name"))
		.toHaveValue("nightly-triage");
	await expect
		.element(dialog.getByRole("radiogroup", { name: "Type" }))
		.toBeVisible();
	await expect
		.element(dialog.getByRole("radiogroup", { name: "Target" }))
		.toBeVisible();
	// The board stays where it was underneath.
	await expect.poll(currentURL).toMatch(/\/system\/watches\/lsegal%2Faviary$/);
	await closeTaskDialog();
});

test("a scheduled card's menu duplicates its task through the shared create form (#1465)", async () => {
	const calls = setup({
		extra: () => ({
			config_get: {
				agents: [
					{
						name: "assistant",
						model: "cli/codex/gpt-5.5",
						channels: [{ type: "discord", id: "ops", name: "ops" }],
						tasks: [
							{
								name: "nightly-triage",
								type: "project",
								project: "lsegal/aviary",
								prompt:
									"Triage issues opened since the last run and label them.",
								model: "cli/claude/opus",
								trigger: "daily",
								at: "02:00",
								timezone: "America/Toronto",
								overlap: "skip",
								missed: "run_once",
								precompile: true,
								compiled: "return 'stale'",
							},
						],
					},
				],
			},
		}),
	});
	await renderApp(BOARD);
	const nightly = page.getByTestId("scheduled-card").first();
	await nightly.getByRole("button", { name: "Duplicate…" }).click();

	const dialog = page.getByRole("dialog", { name: "Duplicate nightly-triage" });
	await expect
		.element(dialog.getByLabelText("Task name"))
		.toHaveValue("nightly-triage-2");
	await expect
		.element(dialog.getByLabelText("Project", { exact: true }))
		.toHaveValue("lsegal/aviary");
	await expect
		.element(dialog.getByLabelText("Prompt"))
		.toHaveValue("Triage issues opened since the last run and label them.");
	await expect
		.element(dialog.getByRole("radio", { name: "Daily" }))
		.toHaveAttribute("aria-checked", "true");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expectHidden(dialog);
	const config = JSON.parse(String(calls.config_save?.[0]?.config)) as {
		agents: { name: string; tasks: Record<string, unknown>[] }[];
	};
	const copy = config.agents[0].tasks.find(
		(t) => t.name === "nightly-triage-2",
	);
	expect(copy).toMatchObject({
		type: "project",
		project: "lsegal/aviary",
		model: "cli/claude/opus",
		trigger: "daily",
		at: "02:00",
		timezone: "America/Toronto",
		overlap: "skip",
		missed: "run_once",
		precompile: true,
	});
	expect(copy).not.toHaveProperty("compiled");
	// The board stays put and lists the copy.
	await expect.poll(currentURL).toMatch(/\/system\/watches\/lsegal%2Faviary$/);
	await expectCount(
		page.getByTestId("scheduled-card").filter({ hasText: "nightly-triage-2" }),
		1,
	);
});

test("a watch no agent owns opens no add-project dialog", async () => {
	setup();
	await renderApp(`${BOARD}?add=1`);
	await expectCount(page.getByTestId("scheduled-card"), 2);
	await expectCount(
		page.getByRole("dialog", { name: "Watch a repository" }),
		0,
	);
});

test("the Board | Schedules toggle and Scheduled Tasks link are gone", async () => {
	setup();
	await renderApp(BOARD);
	await expectCount(page.getByTestId("scheduled-card"), 2);
	await expectCount(page.getByRole("tablist", { name: "Project views" }), 0);
	await expectCount(page.getByRole("tab", { name: /^Schedules/ }), 0);
	// The sidebar's Scheduled Tasks page lists every agent task (#906).
	await expectCount(
		page.getByRole("link", { name: "Manage in Scheduled Tasks" }),
		0,
	);
	await expectCount(page.getByTestId("manage-scheduled-tasks"), 0);
	await expectCount(
		page
			.getByTestId("sidebar-nav")
			.locator('a[href="/agents/assistant/tasks"]'),
		1,
	);
});
