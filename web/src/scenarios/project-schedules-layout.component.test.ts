// Ported from web/e2e/project-schedules.spec.ts (#1636): a scheduled card's
// size and layout beside job cards, through its runs (#875, #948, #1157).
import { describe, expect, test } from "vitest";
import { type Locator, page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { boundingBoxOf } from "./helpers/box";
import {
	BOARD,
	expectCount,
	finishedRun,
	ISSUE_JOB,
	minutesFromNow,
	RELEASE_RUNS,
	RELEASE_TASK,
	releaseEntry,
	schedule,
	setup,
	TASKS,
} from "./project-schedules-fixtures";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/ProjectsView.vue"));

test("scheduled cards are compact: trigger and next run share one line (#875)", async () => {
	setup();
	await renderApp(BOARD);
	const weekly = page.getByTestId("scheduled-card").nth(1);
	await expect.element(weekly.getByTestId("scheduled-trigger")).toBeVisible();
	const details = await boundingBoxOf(
		weekly.getByTestId("scheduled-run-details"),
	);
	const trigger = await boundingBoxOf(weekly.getByTestId("scheduled-trigger"));
	const next = await boundingBoxOf(weekly.getByTestId("scheduled-next"));
	expect(details.height).toBeLessThan(24);
	expect(Math.abs(trigger.y - next.y)).toBeLessThan(4);
});

for (const viewport of [
	{ name: "desktop", width: 1280, height: 900 },
	{ name: "mobile", width: 390, height: 844 },
]) {
	test(`scheduled and job cards share a row's height on ${viewport.name} (#948)`, async () => {
		await page.viewport(viewport.width, viewport.height);
		setup({
			tasks: [TASKS[1]],
			jobs: [ISSUE_JOB],
		});
		await renderApp(BOARD);
		const scheduled = page.getByTestId("scheduled-card");
		const job = page.getByTestId("job-card");
		await expectCount(scheduled, 1);
		await expectCount(job, 1);
		// A never-run schedule still has the output area, with its empty state.
		await expect
			.element(scheduled.getByTestId("scheduled-empty"))
			.toBeVisible();
		const s = await boundingBoxOf(scheduled);
		const j = await boundingBoxOf(job);
		if (viewport.name === "desktop") {
			expect(Math.abs(s.y - j.y)).toBeLessThan(1);
			expect(Math.abs(s.height - j.height)).toBeLessThan(1);
		} else {
			// Stacked on a phone, each card is as tall as a job card's layout.
			expect(Math.abs(s.x - j.x)).toBeLessThan(1);
			expect(s.height).toBeGreaterThan(200);
		}
		// The footer action rows line up at each card's bottom.
		const footerBottom = async (
			card: Locator,
			role: "button" | "link",
			name: string,
		) => {
			const b = await boundingBoxOf(card.getByRole(role, { name }));
			return b.y + b.height;
		};
		const sBottom = await footerBottom(scheduled, "button", "Task settings");
		const jBottom = await footerBottom(job, "link", "Details");
		expect(
			Math.abs(s.y + s.height - sBottom - (j.y + j.height - jBottom)),
		).toBeLessThan(2);
	});
}

// A run changes a scheduled card's contents, never its size: the running
// state sits in the status chip, the output toggle in the output's corner,
// and the footer stays one row (#1157).
describe("a running scheduled card keeps its size (#1157)", () => {
	// The neighbors run either side of the release's next run, in 3m, so it
	// stays the middle card.
	const neighbor = (name: string, minutes: number) =>
		schedule({
			id: `assistant/${name}`,
			name,
			prompt: `Run ${name}.`,
			enabled: true,
			trigger_type: "interval",
			every: "1h",
			description: "every 1h (Local)",
			next_run: minutesFromNow(minutes),
		});
	const TASKS_1157 = [
		neighbor("aviary-find-dupe-tests", 2),
		{ ...RELEASE_TASK, name: "aviary-release", model: "cli/claude/opus" },
		neighbor("aviary-update-docs", 30),
	];
	const RUN = 1795457053;
	const runningEntry = releaseEntry({
		number: RUN,
		key: `lsegal/aviary#${RUN}`,
		status: "active",
		state: "running",
		started: minutesFromNow(-1),
		actions: { retry: false, pause: false, resume: false, stop: true },
		recent_runs: [
			{
				key: `lsegal/aviary#${RUN}`,
				number: RUN,
				status: "active",
				state: "running",
				started: minutesFromNow(-1),
			},
			...RELEASE_RUNS,
		],
	});
	const doneEntry = releaseEntry({
		number: RUN,
		key: `lsegal/aviary#${RUN}`,
		last_run: finishedRun(RUN, 0, "succeeded", "Released v0.7.9"),
		recent_runs: [
			finishedRun(RUN, 0, "succeeded", "Released v0.7.9"),
			...RELEASE_RUNS,
		],
	});

	// The top edge of each visible button in a card's footer.
	const footerButtonTops = (footer: Locator) =>
		[
			...footer
				.element()
				.querySelectorAll<HTMLElement>(":scope > a, :scope > button"),
		]
			.filter((b) => b.offsetParent)
			.map((b) => b.offsetTop);
	type Layout = {
		cards: { x: number; y: number; width: number; height: number }[];
		footerTops: number[];
		gridHeight: number;
	};
	async function layout(): Promise<Layout> {
		const cards = page.getByTestId("scheduled-card");
		const boxes = [];
		for (let i = 0; i < cards.elements().length; i++)
			boxes.push(await boundingBoxOf(cards.nth(i)));
		const footer = cards.nth(1).getByTestId("scheduled-footer");
		await expect.element(footer).toBeInTheDocument();
		const footerTops = footerButtonTops(footer);
		const grid = await boundingBoxOf(
			page.getByRole("region", { name: "Jobs" }),
		);
		return { cards: boxes, footerTops, gridHeight: grid.height };
	}
	function expectSameLayout(actual: Layout, expected: Layout) {
		expect(actual.cards).toHaveLength(expected.cards.length);
		actual.cards.forEach((box, i) => {
			for (const key of ["x", "y", "width", "height"] as const)
				expect(Math.abs(box[key] - expected.cards[i][key])).toBeLessThan(1);
		});
		expect(Math.abs(actual.gridHeight - expected.gridHeight)).toBeLessThan(1);
		// One footer row: every button shares the same offsetTop.
		expect(actual.footerTops.length).toBeGreaterThan(2);
		expect(new Set(actual.footerTops).size).toBe(1);
		expect(actual.footerTops).toEqual(expected.footerTops);
	}

	for (const viewport of [
		{ name: "desktop", width: 1280, height: 900 },
		{ name: "narrow", width: 390, height: 844 },
	]) {
		test(`idle, running and finished cards share one layout on ${viewport.name}`, async () => {
			await page.viewport(viewport.width, viewport.height);
			const entries: unknown[] = [releaseEntry()];
			setup({
				tasks: TASKS_1157,
				entries,
				job: {
					...runningEntry,
					scrollback: "scheduled run started on claude/opus",
					events: [
						{ kind: "message", text: "scheduled run started on claude/opus" },
					],
				},
			});
			await renderApp(BOARD);
			const cards = page.getByTestId("scheduled-card");
			await expectCount(cards, 3);
			const release = cards.nth(1);
			await expect
				.element(release.getByRole("heading"))
				.toHaveText("aviary-release");
			await expect
				.element(release.getByTestId("scheduled-state"))
				.toHaveText("Scheduled");
			const idle = await layout();
			expectSameLayout(idle, idle);

			// The run starts: the chip says so, the output shows with its
			// toggle, and nothing moves.
			entries.splice(0, 1, runningEntry);
			await expect
				.element(release.getByTestId("scheduled-running"))
				.toHaveText(/^\s*Running · [12]m\s*$/);
			await expect
				.element(release.getByTestId("scheduled-output-rich"))
				.toMatchTextContent("scheduled run started on claude/opus");
			await expect.element(release.getByTestId("output-mode")).toBeVisible();
			expectSameLayout(await layout(), idle);

			// It finishes: the card is back to Scheduled, the same size.
			entries.splice(0, 1, doneEntry);
			await expect
				.element(release.getByTestId("scheduled-state"))
				.toHaveText("Scheduled");
			await expect
				.element(release.getByTestId("scheduled-last-summary"))
				.toHaveText("Released v0.7.9");
			expectSameLayout(await layout(), idle);
		});
	}

	for (const width of [1280, 390]) {
		test(`the footer shows each action once as a button, with no menu, at ${width}px (#1486)`, async () => {
			await page.viewport(width, 844);
			const calls = setup({
				tasks: TASKS_1157,
				entries: [runningEntry],
			});
			await renderApp(BOARD);
			const release = page.getByTestId("scheduled-card").nth(1);
			await expect
				.element(release.getByTestId("scheduled-running"))
				.toBeVisible();
			const footer = release.getByTestId("scheduled-footer");
			const actions = ["Run now", "Pause", "Duplicate…", "Task settings"];
			const check = async () => {
				for (const name of actions) {
					await expect
						.element(footer.getByRole("button", { name }))
						.toBeVisible();
				}
				await expectCount(
					release.getByRole("button", { name: "More actions" }),
					0,
				);
				await expectCount(release.getByTestId("scheduled-more"), 0);
				const tops = footerButtonTops(footer);
				// One row: Run now, Pause, Duplicate and the gear, always last.
				expect(tops.length).toBe(4);
				expect(new Set(tops).size).toBe(1);
				const footerBox = await boundingBoxOf(footer);
				const cardBox = await boundingBoxOf(release);
				expect(footerBox.x + footerBox.width).toBeLessThanOrEqual(
					cardBox.x + cardBox.width + 1,
				);
				const gear = await boundingBoxOf(
					footer.getByRole("button", { name: "Task settings" }),
				);
				expect(gear.x + gear.width).toBeLessThanOrEqual(
					cardBox.x + cardBox.width,
				);
			};
			await check();

			// Squeezed narrower than any board column, every action stays a
			// button on one row instead of folding into a menu.
			(release.element() as HTMLElement).style.width = "220px";
			await check();

			await footer.getByRole("button", { name: "Pause" }).click();
			await expect.poll(() => calls.task_update?.length ?? 0).toBe(1);
			expect(calls.task_update[0]).toMatchObject({
				name: "assistant/aviary-release",
				enabled: false,
			});
		});
	}
});
