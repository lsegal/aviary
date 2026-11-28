// Ported from web/e2e/projects.spec.ts (#1636): the project board: status, job cards, their layout and actions.
import { expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { openBoardInfo, openInstanceTag } from "./helpers/boardHeader";
import { boundingBoxOf } from "./helpers/box";
import {
	CLONES,
	JOBS,
	minutesAgo,
	STATUS,
	setup,
	stateChip,
} from "./projects-fixtures";

/** Playwright's isVisible: attached, with a box, and not hidden. */
function isVisible(locator: Locator): boolean {
	const el = locator.query();
	if (!el) return false;
	return (
		el.getClientRects().length > 0 &&
		getComputedStyle(el).visibility !== "hidden"
	);
}

/** Playwright's toBeHidden: absent, or present but not visible. */
async function expectHidden(locator: Locator) {
	await expect.poll(() => isVisible(locator)).toBe(false);
}

test("board shows status, ordered job cards and live output", async () => {
	const calls = setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");

	await expect
		.element(page.getByRole("heading", { name: "lsegal/aviary" }))
		.toBeVisible();
	const repoLink = page
		.getByRole("heading", { name: "lsegal/aviary" })
		.getByRole("link", { name: "lsegal/aviary" });
	await expect
		.element(repoLink)
		.toHaveAttribute("href", "https://github.com/lsegal/aviary");
	await expect.element(repoLink).toHaveAttribute("target", "_blank");
	await expect.element(page.getByTestId("delivery-status")).toHaveText("live");
	await expect
		.poll(() => page.getByTestId("project-owner").elements().length)
		.toBe(0);
	const tag = await openInstanceTag();
	await expect.element(tag.getByTestId("instance-id")).toHaveText("7F3CA91B");
	await commands.keyPress("Escape");
	await expectHidden(tag);
	const info = await openBoardInfo();
	await expect.element(info.getByTestId("board-delivery")).toHaveText("poll");
	await expect
		.element(info.getByTestId("board-interval"))
		.toHaveText("every 30s");
	await expect
		.element(info.getByTestId("board-checked"))
		.toMatchTextContent(/ago/);
	// The header names the issue search pickup runs, never a ready label.
	await expect
		.element(info.getByTestId("issue-filter"))
		.toHaveText("assignee:@me");
	await expect.element(info).toMatchTextContent("Issues matching assignee:@me");
	await expect.element(info).not.toMatchTextContent("labeled");
	await commands.keyPress("Escape");
	const stats = page.getByRole("region", { name: "Job counts" });
	await expect.element(stats).toMatchTextContent("2of 4 busy");
	await expect.element(stats).toMatchTextContent("Merged (24h)3");
	await expect.element(stats).not.toMatchTextContent("Completed");
	await expect.element(stats).not.toMatchTextContent("Waiting");
	await expect
		.poll(() => calls.project_status.at(-1)?.targets)
		.toEqual(["lsegal/aviary"]);
	// Each board state's card counts the jobs its filter chip shows.
	for (const label of ["Starting", "Paused", "Running", "Done", "Failed"]) {
		const card = stats.locator("div").filter({ hasText: label });
		await expect.element(card).toHaveText(new RegExp(`^${label}\\d+$`));
		const count = Number(card.element().textContent?.replace(/\D/g, ""));
		const chip = stateChip(label);
		await chip.click();
		await expect
			.poll(() => page.getByTestId("job-card").elements().length)
			.toBe(count);
		await chip.click();
	}
	const clis = page.getByRole("region", { name: "Models and quota" });
	await expect.element(clis).toMatchTextContent("38% of 5h quota left");
	await expect.element(clis).toMatchTextContent("signed out");
	// A provider/model agent is its provider running a model, not a second
	// provider without a quota reading.
	const claude = clis.locator(":scope > span").filter({ hasText: "claude" });
	await expect.poll(() => claude.elements().length).toBe(1);
	await expect.element(claude).toHaveText(/claudes*opuss*38% of 5h quota left/);
	await expect.element(clis).not.toMatchTextContent("claude/opus");
	await expect.element(clis).not.toMatchTextContent("no quota reported");

	const cards = page.getByTestId("job-card");
	await expect.poll(() => cards.elements().length).toBe(5);
	// Running and errored jobs come first, finished jobs last.
	// #221's agent has not reported its clone yet, so it is still starting.
	await expect.element(cards.nth(0)).toMatchTextContent("Starting");
	await expect
		.element(cards.nth(0).getByTestId("job-state"))
		.toHaveClass(/orange/);
	await expect.element(cards.nth(1)).toMatchTextContent("Running");
	await expect.element(cards.nth(2)).toMatchTextContent("#209");
	await expect.element(cards.nth(4)).toMatchTextContent("Done");
	await expect
		.element(cards.nth(0))
		.toMatchTextContent("gh-fix · codex/gpt-5.5");

	// Fork-aware PR link, waiting reason and checklist progress.
	const paused = cards.filter({ hasText: "#214" });
	await expect
		.element(paused.getByRole("link", { name: /PR someone\/aviary#229/ }))
		.toHaveAttribute("href", "https://github.com/someone/aviary/pull/229");
	await expect
		.element(paused.getByTestId("job-waiting"))
		.toHaveText("Stacked on PR #226, waiting for it to merge");
	await expect
		.element(paused.getByTestId("job-progress"))
		.toHaveAttribute("style", expect.stringMatching(/width: 83%/));

	// Cards stream output and refresh within a few seconds; #221 emits no
	// events, so it shows its scrollback.
	const running = cards.filter({ hasText: "#221" }).getByTestId("job-output");
	await expect
		.element(running)
		.toMatchTextContent("read internal/scheduler/queue.go (#221)");
	const first = running.element().textContent;
	await expect.element(running, { timeout: 5_000 }).not.toHaveText(first ?? "");
});

test("a board shown from the restart cache says it is refreshing until a live poll (#921)", async () => {
	let stale = true;
	setup(() =>
		stale
			? {
					...STATUS,
					stale: true,
					stale_targets: ["lsegal/aviary"],
					cached_at: minutesAgo(3),
				}
			: STATUS,
	);
	await renderApp("/agents/coder/projects/lsegal%2Faviary");

	await openBoardInfo();
	const indicator = page.getByTestId("stale-indicator");
	await expect
		.element(indicator)
		.toMatchTextContent("refreshing… last updated 3m ago");
	// The cached board still renders its stats and cards meanwhile.
	await expect
		.element(page.getByRole("region", { name: "Job counts" }))
		.toMatchTextContent("Merged (24h)3");
	await expect
		.element(
			page.getByText("Cap scheduler retry backoff at 10 minutes").first(),
		)
		.toBeVisible();

	stale = false;
	await expect.poll(() => indicator.elements().length).toBe(0);
});

test("long wrapped output never grows a card past half the viewport (#718)", async () => {
	await page.viewport(1280, 700);
	const line = `$ ${"go test ./internal/scheduler/... -run TestBackoffCap -count=1 ".repeat(6)}`;
	setup(() => STATUS, {
		project_job_get: (args?: Record<string, unknown>) => {
			const found = JOBS.find(
				(j) => j.target === args?.target && j.number === args?.number,
			);
			if (!found) throw new Error("no such job");
			return {
				...found,
				...CLONES[found.number],
				scrollback: Array(12).fill(line).join("\n"),
				events: [],
				raw_output: "",
			};
		},
	});
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	const output = card.getByTestId("job-output");
	await expect.element(output).toMatchTextContent("TestBackoffCap");
	const cardBox = await boundingBoxOf(card);
	const outputBox = await boundingBoxOf(output);
	expect(cardBox?.height).toBeLessThanOrEqual(350);
	// The log area clips its content instead of stretching the card.
	expect(outputBox?.height).toBeLessThan(
		await ((el) => el.scrollHeight)(output.element() as HTMLElement),
	);
	// The header and actions keep their natural size.
	await expect
		.element(card.getByRole("link", { name: /#218 Cap scheduler/ }))
		.toBeInViewport();
	await expect
		.element(card.getByRole("button", { name: "Retry" }))
		.toBeInViewport();
});

test("a job card's wrapped meta line never starts with a separator (#1013)", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	const separator = card.getByTestId("job-meta-separator");
	const pr = card.getByTestId("job-pr");
	await expect.element(pr).toMatchTextContent("PR #233");
	// Squeeze the card until the PR wraps onto its own line.
	(card.element() as HTMLElement).style.width = "90px";
	const top = async (l: typeof pr) => (await boundingBoxOf(l)).y;
	const number = card.getByRole("link", { name: "#218", exact: true });
	await expect.poll(() => top(pr)).toBeGreaterThan(await top(number));
	// The separator stays at the end of the line above it.
	expect(Math.abs((await top(separator)) - (await top(number)))).toBeLessThan(
		4,
	);
});

/** Asserts el's right edge sits at the card's right padding. */
async function expectRightAligned(card: Locator, el: Locator) {
	const c = await boundingBoxOf(card);
	const e = await boundingBoxOf(el);
	expect(c && e).toBeTruthy();
	if (!c || !e) return;
	expect(e.x).toBeGreaterThanOrEqual(c.x);
	expect(c.x + c.width - (e.x + e.width)).toBeLessThanOrEqual(20);
}

for (const [label, width] of [
	["desktop", 1280],
	["mobile", 390],
] as const) {
	test(`job cards put the time top right and the agent label bottom right on ${label} (#756)`, async () => {
		await page.viewport(width, 900);
		setup();
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		const cards = page.getByTestId("job-card");
		await expect.poll(() => cards.elements().length).toBe(5);
		for (const state of ["Starting", "Running", "Failed", "Paused", "Done"]) {
			const card = cards.filter({
				has: page.getByTestId("job-state").getByText(state),
			});
			const time = card.getByTestId("job-time");
			const agent = card.getByTestId("job-agent");
			const title = card.getByRole("heading");
			await expect.element(time).toHaveText(/\d+(m|h)/);
			await expect
				.element(time)
				.toHaveAttribute("title", expect.stringMatching(/^Started /));
			await expect.element(agent).toHaveText(/^gh-fix · /);
			await expect
				.element(agent)
				.toHaveAttribute("title", expect.stringMatching(/^gh-fix · /));
			await expectRightAligned(card, time);
			await expectRightAligned(card, agent);
			const timeBox = await boundingBoxOf(time);
			const titleBox = await boundingBoxOf(title);
			const agentBox = await boundingBoxOf(agent);
			const retryBox = await boundingBoxOf(
				card.getByRole("button", { name: "Retry" }),
			);
			// The time stays in the header row above the title; the label sits
			// in the footer beside the actions.
			expect(timeBox.y + timeBox.height).toBeLessThanOrEqual(titleBox.y);
			expect(agentBox.y).toBeGreaterThan(titleBox.y + titleBox.height);
			expect(Math.abs(agentBox.y - retryBox.y)).toBeLessThan(retryBox.height);
		}
	});
}

test("a long agent label truncates inside the card with a tooltip (#756)", async () => {
	await page.viewport(390, 900);
	const model = `claude/${"very-long-model-name-".repeat(6)}opus`;
	const jobs = JOBS.filter((j) => j.number === 218).map((j) => ({
		...j,
		model,
	}));
	setup(() => STATUS, {
		project_job_list: () => ({ jobs, total: 1, offset: 0, limit: 50 }),
	});
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card");
	await expect.poll(() => card.elements().length).toBe(1);
	const agent = card.getByTestId("job-agent");
	await expect
		.element(agent)
		.toHaveAttribute("title", expect.stringMatching(new RegExp(`${model}$`)));
	await expect.element(agent).toHaveCSS("text-overflow", "ellipsis");
	await expect.element(agent).toHaveCSS("white-space", "nowrap");
	const truncated = await ((el) => el.scrollWidth > el.clientWidth)(
		agent.element() as HTMLElement,
	);
	expect(truncated).toBe(true);
	await expectRightAligned(card, agent);
	const cardBox = await boundingBoxOf(card);
	const agentBox = await boundingBoxOf(agent);
	expect(agentBox.x + agentBox.width).toBeLessThanOrEqual(
		cardBox.x + cardBox.width,
	);
	// One line tall, and the title keeps the full header width.
	expect(agentBox.height).toBeLessThan(24);
	const title = card.getByRole("heading");
	const titleBox = await boundingBoxOf(title);
	expect(titleBox.width).toBeGreaterThan(cardBox.width - 40);
});

test("retry, stop, filters and check now call project tools", async () => {
	const calls = setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const card = page.getByTestId("job-card").filter({ hasText: "#218" });
	await card.getByRole("button", { name: "Retry" }).click();
	await expect
		.poll(() => calls.project_job_retry?.[0])
		.toEqual({ target: "lsegal/aviary", number: 218 });
	await card.getByRole("button", { name: "Stop" }).click();
	await expect
		.poll(() => calls.project_job_stop?.[0])
		.toEqual({ target: "lsegal/aviary", number: 218 });
	// The engine's actions decide what each card offers.
	const done = page.getByTestId("job-card").filter({ hasText: "#200" });
	await expect
		.poll(() => done.getByRole("button", { name: "Stop" }).elements().length)
		.toBe(0);

	await stateChip("Paused").click();
	await expect
		.poll(() => page.getByTestId("job-card").elements().length)
		.toBe(1);
	await expect.element(page.getByTestId("job-card")).toMatchTextContent("#214");
	await expect
		.poll(() =>
			calls.project_job_list.some(
				(c) => JSON.stringify(c.states) === '["paused"]',
			),
		)
		.toBe(true);

	// Refresh lives in the header's "…" menu (#1488).
	await expect
		.poll(
			() => page.getByRole("button", { name: "Check now" }).elements().length,
		)
		.toBe(0);
	await page.getByTestId("board-more-menu").click();
	await page.getByRole("menuitem", { name: "Refresh" }).click();
	await expect
		.poll(() => calls.project_refresh?.[0])
		.toEqual({ target: "lsegal/aviary" });
});

test("done and failed jobs clear from their card and in bulk (#1460)", async () => {
	const calls = setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	// Only finished cards offer Clear.
	const running = page.getByTestId("job-card").filter({ hasText: "#218" });
	await expect
		.poll(() => running.getByTestId("job-clear").elements().length)
		.toBe(0);
	const done = page.getByTestId("job-card").filter({ hasText: "#200" });
	await done.getByRole("button", { name: "Clear job" }).click();
	await expect
		.poll(() => calls.project_job_clear?.[0])
		.toEqual({ target: "lsegal/aviary", number: 200 });

	// The header's menu clears the project's failed jobs after a confirmation.
	// It lives in the "…" menu, not as a trash button (#1488).
	await expect
		.poll(() => page.getByTestId("clear-jobs-menu").elements().length)
		.toBe(0);
	await page.getByTestId("board-more-menu").click();
	await expect
		.element(page.getByTestId("board-more-items"))
		.toMatchTextContent("Clear this project's jobs");
	await page.getByTestId("clear-jobs-failed").click();
	const confirm = page.getByTestId("clear-jobs-confirm");
	await expect
		.element(confirm)
		.toMatchTextContent("failed jobs of lsegal/aviary");
	await expect
		.element(confirm)
		.toMatchTextContent("Running, queued and paused jobs are never cleared.");
	await page.getByTestId("clear-jobs-confirm-confirm").click();
	await expect.poll(() => confirm.elements().length).toBe(0);
	await expect
		.poll(() => calls.project_job_clear?.[1])
		.toEqual({ targets: ["lsegal/aviary"], which: "failed" });
});
