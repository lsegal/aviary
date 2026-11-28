// Ported from web/e2e/project-schedules.spec.ts (#1636): a scheduled card's
// runs, one-time and recurring, shown in place on its one card (#875, #977).
import { describe, expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, goBack, reload, renderApp } from "../component-test/app";
import {
	BOARD,
	expectCount,
	finishedRun,
	ISSUE_JOB,
	minutesFromNow,
	RELEASE_RUNS,
	RELEASE_TASK,
	releaseEntry,
	SCHEDULED_JOB,
	schedule,
	setup,
} from "./project-schedules-fixtures";

// A one-time schedule moves through these states: armed, fired and running
// (the scheduler lists no next run once it fires), then finished.
const ONCE_AT = minutesFromNow(-1);
const onceTask = (fields: Record<string, unknown>) =>
	schedule({
		id: "assistant/one-off",
		name: "one-off",
		prompt: "Summarize this week's merged pull requests.",
		enabled: true,
		trigger_type: "once",
		start_at: ONCE_AT,
		description: "once at 2026-11-21 19:11 (Local)",
		...fields,
	});
const ONCE_JOB = {
	...SCHEDULED_JOB,
	key: "lsegal/aviary#1795424500",
	number: 1795424500,
	title: "one-off",
	schedule_id: "assistant/one-off",
	scrollback: "Collecting merged pull requests…",
	events: [{ kind: "message", text: "Found 12 merged pull requests." }],
};

const ONCE_RUN = {
	key: ONCE_JOB.key,
	number: ONCE_JOB.number,
	started: ONCE_AT,
};

for (const tc of [
	{
		state: "before",
		task: onceTask({ next_run: minutesFromNow(30), history: [] }),
		entries: [],
		next: /^Next run in (29|30)m \(.+\)$/,
		running: false,
	},
	{
		state: "during",
		task: onceTask({
			history: [{ fire_time: ONCE_AT, reason: "schedule", status: "running" }],
		}),
		entries: [
			{
				...ONCE_JOB,
				state: "running",
				next_run: { paused: false, queued: false },
				recent_runs: [{ ...ONCE_RUN, status: "active", state: "running" }],
			},
		],
		next: null,
		running: true,
	},
	{
		state: "after",
		task: onceTask({
			history: [
				{
					fire_time: ONCE_AT,
					reason: "schedule",
					status: "completed",
					summary: "Posted the weekly summary",
				},
			],
		}),
		entries: [
			{
				...ONCE_JOB,
				status: "complete",
				state: "scheduled",
				actions: {},
				next_run: { paused: false, queued: false },
				last_run: {
					...ONCE_RUN,
					status: "complete",
					state: "done",
					result: "succeeded",
					summary: "Posted the weekly summary",
					finished: ONCE_AT,
				},
				recent_runs: [
					{
						...ONCE_RUN,
						status: "complete",
						state: "done",
						result: "succeeded",
						summary: "Posted the weekly summary",
					},
				],
			},
		],
		next: /^No next run$/,
		running: false,
	},
]) {
	test(`a one-time schedule's next run ${tc.state} its run (#875)`, async () => {
		setup({
			tasks: [tc.task],
			entries: tc.entries,
			job: tc.entries[0],
		});
		await renderApp(BOARD);
		const card = page.getByTestId("scheduled-card");
		await expectCount(card, 1);
		await expect
			.element(card.getByTestId("scheduled-trigger"))
			.toHaveText("once at 2026-11-21 19:11 (Local)");
		await expect.element(card).not.toMatchTextContent("none");
		if (tc.next)
			await expect
				.element(card.getByTestId("scheduled-next"))
				.toHaveText(tc.next);
		else await expectCount(card.getByTestId("scheduled-next"), 0);
		await expectCount(
			card.getByTestId("scheduled-running"),
			tc.running ? 1 : 0,
		);

		const kanbanNext = async () => {
			await page.getByTestId("kanban-toggle").click();
			const next = page.getByTestId("kanban-scheduled-next");
			await expect.element(next).toBeInTheDocument();
			const text = next.element().textContent;
			await page.getByTestId("kanban-toggle").click();
			return text?.trim();
		};
		if (tc.state === "during") {
			await expect
				.element(card.getByTestId("scheduled-running"))
				.toHaveText(/^\s*Running · \d+[sm]\s*$/);
			await expectCount(card.getByTestId("scheduled-last"), 0);
			// The run's output shows in rich and raw mode, like an issue card.
			await expect
				.element(card.getByTestId("scheduled-output-rich"))
				.toMatchTextContent("Found 12 merged pull requests.");
			await card.getByRole("button", { name: "Raw output" }).click();
			await expect
				.element(card.getByTestId("scheduled-output"))
				.toHaveText("Collecting merged pull requests…");
			await card.getByRole("button", { name: "Rich output" }).click();
			expect(await kanbanNext()).toBe("Running now");
		}
		if (tc.state === "after") {
			// The card is back to Scheduled with the finished run's result and
			// summary in place of its output (#977).
			await expect
				.element(card.getByTestId("scheduled-state"))
				.toHaveText("Scheduled");
			await expectCount(card.getByTestId("scheduled-output-rich"), 0);
			await expect
				.element(card.getByTestId("scheduled-last-summary"))
				.toHaveText("Posted the weekly summary");
			await expect
				.element(card.getByTestId("scheduled-last-result"))
				.toHaveText("✓");
			expect(await kanbanNext()).toBe("No next run");
		}
		if (tc.state === "before") {
			await expect
				.element(card.getByTestId("scheduled-last"))
				.toHaveText(/^\s*Hasn't run yet · next run in (29|30)m\s*$/);
			await expectCount(card.getByTestId("scheduled-output"), 0);
		}
	});
}

describe("one card per scheduled task (#977)", () => {
	test("several completed runs leave one card with the last and next run, and no Done copies", async () => {
		const calls = setup({
			tasks: [RELEASE_TASK],
			entries: [releaseEntry()],
			jobs: [ISSUE_JOB],
		});
		await renderApp(BOARD);
		const card = page.getByTestId("scheduled-card");
		await expectCount(card, 1);
		// The runs are not job cards: only the issue's is.
		await expectCount(page.getByTestId("job-card"), 1);
		await expect
			.element(page.getByTestId("job-card"))
			.toMatchTextContent("Fix the parser");
		await expect
			.element(card.getByTestId("scheduled-state"))
			.toHaveText("Scheduled");
		await expect
			.element(card.getByTestId("scheduled-next"))
			.toHaveText(/^Next run in [23]m \(.+\)$/);
		await expect
			.element(card.getByTestId("scheduled-last-result"))
			.toHaveText("✓");
		await expect
			.element(card.getByTestId("scheduled-last-time"))
			.toHaveText(/^Last run 2m ago · succeeded$/);
		await expect
			.element(card.getByTestId("scheduled-last-summary"))
			.toHaveText("main matches the latest release, so no release was started");
		// The strip shows recent results, oldest first.
		await expect
			.element(card.getByTestId("scheduled-strip"))
			.toHaveText("✓✖⚠✓");
		// The project's jobs are read apart from its schedules' entries.
		expect(calls.project_job_list).toContainEqual(
			expect.objectContaining({
				kinds: ["issue", "discussion", "merge", "discuss"],
			}),
		);

		// Done shows neither the schedule nor its runs.
		const filter = page.getByRole("group", { name: "Job filter" });
		await filter.getByRole("button", { name: /^Done/ }).click();
		await expectCount(card, 0);
		await expectCount(page.getByTestId("job-card"), 0);
		await filter.getByRole("button", { name: /^Done/ }).click();

		// So does the kanban: one Scheduled box and no Done column cards.
		await page.getByTestId("kanban-toggle").click();
		await expectCount(page.getByTestId("kanban-scheduled-card"), 1);
		await expect
			.element(
				page
					.getByTestId("kanban-scheduled-card")
					.getByTestId("kanban-scheduled-last"),
			)
			.toHaveText(/✓\s*Last run 2m ago/);
		await expectCount(
			page
				.locator('[data-testid="kanban-column"][data-state="done"]')
				.getByTestId("kanban-card"),
			0,
		);
	});

	test("a run shows Running on the same card, which then returns to Scheduled", async () => {
		const running = releaseEntry({
			number: 1795457053,
			key: "lsegal/aviary#1795457053",
			status: "active",
			state: "running",
			started: minutesFromNow(-1),
			actions: { retry: false, pause: false, resume: false, stop: true },
			recent_runs: [
				{
					key: "lsegal/aviary#1795457053",
					number: 1795457053,
					status: "active",
					state: "running",
					started: minutesFromNow(-1),
				},
				...RELEASE_RUNS,
			],
		});
		const entries: unknown[] = [running];
		setup({
			tasks: [RELEASE_TASK],
			entries,
			job: {
				...running,
				scrollback: "Comparing main with v0.7.8…",
				events: [{ kind: "message", text: "Comparing main with v0.7.8…" }],
			},
		});
		await renderApp(BOARD);
		const card = page.getByTestId("scheduled-card");
		await expectCount(card, 1);
		await expect
			.element(card.getByTestId("scheduled-state"))
			.toHaveText("Running");
		await expect.element(card.getByTestId("scheduled-spinner")).toBeVisible();
		await expect
			.element(card.getByTestId("scheduled-elapsed"))
			.toHaveText(/^[12]m$/);
		await expect
			.element(card.getByTestId("scheduled-output-rich"))
			.toMatchTextContent("Comparing main with v0.7.8…");
		// The last run stays on the card while the next one runs.
		await expect
			.element(card.getByTestId("scheduled-last-time"))
			.toHaveText(/^Last run 2m ago/);
		await expectCount(page.getByTestId("job-card"), 0);
		// Running counts it; the card stays where it is.
		const filter = page.getByRole("group", { name: "Job filter" });
		await filter.getByRole("button", { name: /^Running/ }).click();
		await expectCount(card, 1);
		await filter.getByRole("button", { name: /^Running/ }).click();

		// The run finishes: the same card is back to Scheduled with it as its
		// last run, and nothing is left behind.
		entries.splice(
			0,
			1,
			releaseEntry({
				number: 1795457053,
				key: "lsegal/aviary#1795457053",
				last_run: finishedRun(1795457053, 0, "succeeded", "Released v0.7.9"),
				recent_runs: [
					finishedRun(1795457053, 0, "succeeded", "Released v0.7.9"),
					...RELEASE_RUNS,
				],
			}),
		);
		await expect
			.element(card.getByTestId("scheduled-state"))
			.toHaveText("Scheduled");
		await expectCount(card, 1);
		await expectCount(card.getByTestId("scheduled-output-rich"), 0);
		await expect
			.element(card.getByTestId("scheduled-last-summary"))
			.toHaveText("Released v0.7.9");
		await expectCount(page.getByTestId("job-card"), 0);
	});

	test("a failed last run shows on the card and under Failed until dismissed", async () => {
		const failed = finishedRun(1795457053, 1, "failed", "gh: HTTP 502");
		const calls = setup({
			tasks: [RELEASE_TASK],
			entries: [
				releaseEntry({
					number: 1795457053,
					key: "lsegal/aviary#1795457053",
					status: "failed",
					state: "failed",
					last_run: failed,
					recent_runs: [failed, ...RELEASE_RUNS],
				}),
			],
		});
		await renderApp(BOARD);
		const card = page.getByTestId("scheduled-card");
		await expect
			.element(card.getByTestId("scheduled-failed"))
			.toHaveText("Failed");
		await expect
			.element(card.getByTestId("scheduled-last-result"))
			.toHaveText("✖");
		await expect
			.element(card.getByTestId("scheduled-last-summary"))
			.toHaveText("gh: HTTP 502");
		const filter = page.getByRole("group", { name: "Job filter" });
		await filter.getByRole("button", { name: /^Failed/ }).click();
		await expectCount(card, 1);
		await card.getByTestId("scheduled-dismiss").click();
		await expect
			.poll(() => calls.project_job_dismiss?.[0])
			.toEqual({ target: "lsegal/aviary", number: 1795457053 });
	});

	test("Last run counts manual runs, skips missed fires and opens the run (#1013)", async () => {
		// The scheduler's history, oldest first: a scheduled run 6h ago, a
		// missed fire after it, then a manual run a minute ago that the
		// board's entry has not caught up with.
		const fire = (minutesAgo: number, fields: Record<string, unknown>) => ({
			fire_time: minutesFromNow(-minutesAgo),
			...fields,
		});
		const history = [
			fire(365, {
				reason: "schedule",
				status: "completed",
				summary: "Nothing to release",
			}),
			fire(360, { reason: "missed", status: "missed" }),
		];
		const sixHoursAgo = finishedRun(
			1795456753,
			364,
			"succeeded",
			"Nothing to release",
		);
		const entries: unknown[] = [
			releaseEntry({ last_run: sixHoursAgo, recent_runs: [sixHoursAgo] }),
		];
		setup({ tasks: [{ ...RELEASE_TASK, history }], entries });
		await page.viewport(420, 900);
		await renderApp(BOARD);
		const card = page.getByTestId("scheduled-card");
		const line = card.getByTestId("scheduled-last-run");

		// The missed fire is not a run: the last run is the scheduled one,
		// with its result, and it opens that run.
		await expect
			.element(card.getByTestId("scheduled-last-result"))
			.toHaveText("✓");
		await expect
			.element(card.getByTestId("scheduled-last-time"))
			.toHaveText(/^Last run 6h ago · succeeded$/);
		await card.getByTestId("scheduled-last-time").click();
		await expect.poll(currentURL).toMatch(/\/jobs\/1795456753/);
		await goBack();

		// A manual run finishes: the card shows it, newest of any trigger.
		history.push(
			fire(1, {
				reason: "manual",
				status: "completed",
				summary: "Triggered release.yml on main",
			}),
		);
		await reload();
		await expect
			.element(card.getByTestId("scheduled-last-time"))
			.toHaveText(/^Last run 1m ago · succeeded$/);
		await expect.element(line).not.toHaveText(/^\s*[·•]/);
	});

	test("a missed fire never shows as the last run while a manual run is in flight (#1013)", async () => {
		// The issue's history, oldest first: the last scheduled run 6h ago, a
		// missed fire, and a manual run still running, which the board has
		// no finished run for yet.
		const history = [
			{
				fire_time: minutesFromNow(-365),
				reason: "schedule",
				status: "completed",
				summary: "Nothing to release",
			},
			{ fire_time: minutesFromNow(-360), reason: "missed", status: "missed" },
			{ fire_time: minutesFromNow(-1), reason: "manual", status: "running" },
		];
		setup({ tasks: [{ ...RELEASE_TASK, history }], entries: [] });
		await renderApp(BOARD);
		const line = page
			.getByTestId("scheduled-card")
			.getByTestId("scheduled-last-run");
		await expect
			.element(line)
			.toHaveText(/^\s*✓\s*Last run 6h ago · succeeded$/);
		await expect.element(line).not.toHaveText(/^\s*[·•]/);
	});

	test("Paused and Queued take the next run's place", async () => {
		setup({
			tasks: [
				{ ...RELEASE_TASK, enabled: false },
				schedule({
					id: "assistant/sync",
					name: "sync",
					prompt: "Sync labels.",
					enabled: true,
					every: "1m",
					description: "every 1m (Local)",
					next_run: minutesFromNow(1),
				}),
			],
			entries: [
				releaseEntry({ next_run: { paused: true, queued: false } }),
				releaseEntry({
					key: "lsegal/aviary#1795457000",
					number: 1795457000,
					title: "sync",
					schedule_id: "assistant/sync",
					status: "active",
					state: "running",
					next_run: { at: minutesFromNow(1), paused: false, queued: true },
					recent_runs: [
						{
							key: "lsegal/aviary#1795457000",
							number: 1795457000,
							status: "active",
							state: "running",
							started: minutesFromNow(-3),
						},
					],
					last_run: undefined,
				}),
			],
		});
		await renderApp(BOARD);
		const cards = page.getByTestId("scheduled-card");
		await expectCount(cards, 2);
		const release = cards.filter({ hasText: "release-aviary" });
		await expect
			.element(release.getByTestId("scheduled-next"))
			.toHaveText("Next run: paused");
		await expect.element(release.getByTestId("paused-chip")).toBeVisible();
		await expect
			.element(cards.filter({ hasText: "sync" }).getByTestId("scheduled-next"))
			.toHaveText("Queued");
	});
});
