// Ported from web/e2e/project-schedules.spec.ts (#1636): a Code Loop board's
// up next strip of runs due soon.
import { afterEach, expect, test, vi } from "vitest";
import { page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { boundingBoxOf } from "./helpers/box";
import {
	AGENT_BOARD,
	BOARD,
	expectCount,
	expectTexts,
	minutesFromNow,
	setup,
	upcomingRun,
} from "./project-schedules-fixtures";
// The pages this file renders (see component-test/routes.ts).
import "../views/ProjectsView.vue";
import "../views/SettingsView.vue";
import "../views/TaskView.vue";

afterEach(() => {
	vi.useRealTimers();
});

test("a project board shows the up next strip", async () => {
	const calls = setup({
		upcoming: [
			upcomingRun("nightly-triage", 3),
			upcomingRun("nightly-triage", 3 + 1440),
		],
	});
	await renderApp(BOARD);

	const strip = page.getByTestId("up-next");
	// One chip per task: the second nightly fire isn't listed.
	await expectCount(strip.getByTestId("up-next-run"), 1);
	await expect
		.element(strip.getByTestId("up-next-run").first())
		.toMatchTextContent("nightly-triage");
	expect(calls.task_upcoming?.[0]).toEqual({
		project: "lsegal/aviary",
		limit: 50,
		per_task: 1,
	});

	// A run in the strip opens its task's page, as its card does.
	await strip.getByTestId("up-next-run").first().click();
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/tasks\/nightly-triage$/);
});

test("up next lists only runs due within five minutes, as time passes (#1577)", async () => {
	// As page.clock.install: the page's clock, which a test moves on.
	vi.useFakeTimers({
		toFake: ["Date", "setInterval", "clearInterval"],
		shouldAdvanceTime: true,
	});
	setup({
		upcoming: [upcomingRun("soon", 3), upcomingRun("later", 10)],
	});
	await renderApp(BOARD);

	const chips = page.getByTestId("up-next").getByTestId("up-next-run");
	await expectTexts(chips, [/soon/]);

	// Six minutes on, the 10-minute run is within the window and the 3-minute
	// one has fired, with no reload.
	await vi.advanceTimersByTimeAsync(6 * 60_000);
	await expectTexts(chips, [/later/]);

	// Once the later run fires too, nothing is due and the strip goes.
	await vi.advanceTimersByTimeAsync(5 * 60_000);
	await expectCount(page.getByTestId("up-next"), 0);
});

test("up next is hidden on a phone when nothing is due within five minutes (#1577)", async () => {
	await page.viewport(375, 812);
	setup({ upcoming: [upcomingRun("later", 10)] });
	await renderApp(BOARD);
	await expectCount(page.getByTestId("scheduled-card"), 2);
	await expectCount(page.getByTestId("up-next"), 0);
});

// Fires of a task every 30 seconds, every minute, and nightly, interleaved
// the way a server listing several fires per task returns them.
const FREQUENT_UPCOMING = [
	["assistant/every-30s", "every-30s", 0.5],
	["assistant/every-minute", "every-minute", 0.8],
	["assistant/every-30s", "every-30s", 1],
	["assistant/every-minute", "every-minute", 1.8],
	["assistant/every-30s", "every-30s", 1.5],
	["assistant/every-minute", "every-minute", 2.8],
	["assistant/nightly-triage", "nightly-triage", 4],
].map(([task_id, name, m]) => ({
	task_id,
	name,
	project: "lsegal/aviary",
	at: minutesFromNow(m as number),
}));

test("up next shows one chip per task, soonest first (#1574)", async () => {
	setup({ upcoming: FREQUENT_UPCOMING });
	await renderApp(BOARD);

	const chips = page.getByTestId("up-next").getByTestId("up-next-run");
	await expectCount(chips, 3);
	await expect.element(chips.nth(0)).toMatchTextContent("every-30s");
	await expect.element(chips.nth(1)).toMatchTextContent("every-minute");
	await expect.element(chips.nth(2)).toMatchTextContent("nightly-triage");
});

test("up next stays one row on a phone (#1574)", async () => {
	await page.viewport(375, 812);
	setup({
		upcoming: [
			...FREQUENT_UPCOMING,
			...["weekly-report", "dependency-sweep", "stale-issues"].map(
				(name, i) => ({
					task_id: `assistant/${name}`,
					name,
					project: "lsegal/aviary",
					at: minutesFromNow(3.2 + i / 5),
				}),
			),
		],
	});
	await renderApp(BOARD);

	const strip = page.getByTestId("up-next");
	const chips = strip.getByTestId("up-next-run");
	await expectCount(chips, 6);
	// Every chip sits on the strip's one row; extra chips scroll sideways
	// rather than wrapping the strip onto more rows.
	const tops = new Set<number>();
	for (const chip of chips.all()) {
		tops.add(Math.round((await boundingBoxOf(chip)).y));
	}
	expect(tops.size).toBe(1);
	const box = await boundingBoxOf(strip);
	const chipBox = await boundingBoxOf(chips.first());
	expect(box.height).toBeLessThan(chipBox.height * 2);
});

test("up next strip is hidden when no schedule is enabled", async () => {
	setup({ upcoming: [] });
	await renderApp(BOARD);
	await expectCount(page.getByTestId("scheduled-card"), 2);
	await expectCount(page.getByTestId("up-next"), 0);
});

test("the agent board shows its projects' runs due within five minutes in up next (#1577)", async () => {
	const calls = setup({
		...AGENT_BOARD,
		upcoming: [
			upcomingRun("zvid-bench", 2, "lsegal/zvidlib"),
			upcomingRun("nightly-triage", 4),
			upcomingRun("elsewhere", 1, "someone/else"),
			upcomingRun("weekly-deps", 30),
		],
	});
	await renderApp("/agents/coder/projects");

	const chips = page.getByTestId("up-next").getByTestId("up-next-run");
	// Only the board's projects' runs, and only those due within the window.
	await expectTexts(chips, [/zvid-bench/, /nightly-triage/]);
	await expect
		.element(chips.first())
		.toHaveAttribute("title", expect.stringMatching(/^lsegal\/zvidlib · /));
	// A board of several projects asks for every task's runs.
	expect(calls.task_upcoming?.[0]).toEqual({ limit: 50, per_task: 1 });
});
