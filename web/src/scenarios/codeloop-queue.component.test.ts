import { expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { BOARD, JOBS, setup, TARGET } from "./codeloop-queue.fixtures";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/ProjectsView.vue"));

// Queueing pull requests for gh-safe-merge and Discussion threads for
// gh-discuss from a Code Loop board (#1095). Fixtures follow the payloads
// internal/projects' Manager returns. The `aviary watch` session case lives
// in codeloop-queue-watch.component.test.ts: the signed-out health check it
// loads is module state that would carry into the tests after it.

const count = (loc: Locator) => loc.elements().length;
const texts = (loc: Locator) =>
	loc
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());

test("the header's Queue menu offers Fix issue, Safe merge PR and Discuss", async () => {
	setup();
	await renderApp(BOARD);
	await page.getByTestId("queue-menu").click();
	const items = page.getByTestId("queue-menu-items");
	await expect
		.poll(() => texts(items.getByRole("menuitem")))
		.toEqual([
			expect.stringMatching(/Fix issue/),
			expect.stringMatching(/Safe merge PR/),
			expect.stringMatching(/Discuss/),
		]);
});

test("the header's Queue button looks like its neutral icon neighbors (#1305)", async () => {
	setup();
	const queue = page.getByTestId("queue-menu");
	const settings = page.getByTestId("board-more-menu");
	const look = (el: Element) => {
		const s = getComputedStyle(el);
		const r = el.getBoundingClientRect();
		return {
			background: s.backgroundColor,
			border: s.borderColor,
			borderWidth: s.borderWidth,
			radius: s.borderRadius,
			color: s.color,
			padding: s.padding,
			width: r.width,
			height: r.height,
		};
	};
	for (const colorScheme of ["light", "dark"] as const) {
		await commands.emulateMedia({ colorScheme });
		await renderApp(BOARD);
		await expect.element(queue).toHaveAttribute("aria-label", "Queue…");
		await expect.element(queue).toHaveAttribute("title", "Queue…");
		await expect.element(queue).toHaveText("");
		await expect.element(settings).toBeVisible();
		await expect
			.poll(() => look(queue.element()))
			.toEqual(look(settings.element()));
		await settings.hover();
		const hovered = look(settings.element());
		await queue.hover();
		await expect.poll(() => look(queue.element())).toEqual(hovered);
	}
});

test("the safe-merge picker lists open PRs and queues the picked ones in order", async () => {
	const calls = setup();
	await renderApp(BOARD);
	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-safe-merge").click();
	const dialog = page.getByTestId("queue-dialog");
	await expect.element(dialog).toHaveAttribute("data-skill", "gh-safe-merge");
	const rows = dialog.getByTestId("queue-candidate");
	await expect.poll(() => count(rows)).toBe(4);
	const typo = rows.filter({ hasText: "#1210" });
	await expect.element(typo).toMatchTextContent("octocat");
	await expect.element(typo).toMatchTextContent("passing");
	await expect.element(typo).toMatchTextContent("approved");
	await expect.element(typo).toMatchTextContent("clean");
	await expect
		.element(rows.filter({ hasText: "#1212" }))
		.toMatchTextContent("conflicts");
	// A pull request a job already owns names the job and cannot be picked.
	const owned = rows.filter({ hasText: "#1207" });
	await expect
		.element(owned.getByTestId("queue-owned"))
		.toHaveText(`${TARGET}#1207`);
	await expect.element(owned).toHaveAttribute("aria-disabled", "true");
	// As Playwright's click({ force: true }): click it despite aria-disabled.
	(owned.element() as HTMLElement).click();
	await expect.poll(() => count(owned.getByTestId("queue-order"))).toBe(0);

	// Picking order is merge order.
	await rows.filter({ hasText: "#1212" }).click();
	await rows.filter({ hasText: "#1210" }).click();
	await expect
		.element(rows.filter({ hasText: "#1212" }).getByTestId("queue-order"))
		.toHaveText("1");
	await expect
		.element(rows.filter({ hasText: "#1210" }).getByTestId("queue-order"))
		.toHaveText("2");

	// Search narrows the list.
	await dialog.getByTestId("queue-search").fill("typo");
	await expect.poll(() => count(rows)).toBe(1);
	await dialog.getByTestId("queue-search").fill("");

	await dialog.getByTestId("queue-submit").click();
	await expect
		.poll(() => calls.project_job_queue?.[0])
		.toEqual({
			target: TARGET,
			skill: "gh-safe-merge",
			numbers: [1212, 1210],
			titles: { "1210": "Fix typo in README", "1212": "Speed up the job list" },
		});
	await expect
		.element(dialog.getByTestId("queue-results"))
		.toMatchTextContent("#1212: queued, priority #3");
});

test("merge and discuss jobs show as board cards with their place and reason", async () => {
	setup();
	await renderApp(BOARD);
	await expect
		.poll(() => count(page.getByTestId("job-card")), { timeout: 15_000 })
		.toBe(JOBS.length);
	const running = page
		.getByTestId("job-card")
		.filter({ hasText: "Add a dark theme toggle" });
	await expect
		.element(running.getByTestId("job-kind"))
		.toHaveText("Safe merge");
	await expect
		.element(running.getByTestId("job-queue-position"))
		.toHaveText("merging now");
	await expect
		.element(running.getByRole("link", { name: "PR #1201" }))
		.toHaveAttribute("href", `https://github.com/${TARGET}/pull/1201`);
	await expect
		.element(running.getByRole("button", { name: "Stop" }))
		.toBeVisible();

	const queued = page.getByTestId("job-card").filter({ hasText: "Bump vite" });
	await expect
		.element(queued.getByTestId("job-queue-position"))
		.toHaveText("queued · priority #1");
	await expect
		.element(queued.getByRole("button", { name: "Retry" }))
		.toBeDisabled();

	const failed = page
		.getByTestId("job-card")
		.filter({ hasText: "Rewrite the poller" });
	await expect
		.element(failed.getByTestId("job-reason"))
		.toHaveText("Skipped: the pull request conflicts with main");
	await expect
		.element(failed.getByRole("button", { name: "Retry" }))
		.toBeEnabled();

	const discuss = page
		.getByTestId("job-card")
		.filter({ hasText: "watch a Project board" });
	await expect.element(discuss.getByTestId("job-kind")).toHaveText("Discuss");
	await expect
		.element(discuss.getByRole("link", { name: "Discussion #44" }))
		.toHaveAttribute("href", `https://github.com/${TARGET}/discussions/44`);
	await expect
		.element(discuss.getByTestId("job-agent"))
		.toMatchTextContent("gh-discuss");
});

// The stat cards and chips count queued merge and discuss jobs, so the list
// shows every one of them under each chip (#1423).
for (const [board, path] of [
	["a project board", BOARD],
	["the root board", "/agents/coder/projects"],
]) {
	test(`each state chip on ${board} lists as many cards as it counts, merge and discuss jobs included`, async () => {
		setup();
		await renderApp(path);
		const cards = page.getByTestId("job-card");
		await expect
			.poll(() => count(cards), { timeout: 15_000 })
			.toBe(JOBS.length);
		const filter = page.getByRole("group", { name: "Job filter" });
		for (const [state, label] of [
			["starting", "Starting"],
			["running", "Running"],
			["done", "Done"],
			["failed", "Failed"],
		]) {
			const chip = filter.getByRole("button", {
				name: new RegExp(`^${label}`),
			});
			const n = JOBS.filter((j) => j.state === state).length;
			await expect.element(chip).toHaveText(`${label}${n}`);
			await chip.click();
			await expect.element(chip).toHaveAttribute("aria-pressed", "true");
			await expect.poll(() => count(cards)).toBe(n);
			await chip.click();
			await expect.element(chip).toHaveAttribute("aria-pressed", "false");
		}

		// Starting lists the queued merge, with no agent or model yet, at its
		// place in the queue, and the queued thread.
		await filter.getByRole("button", { name: /^Starting/ }).click();
		await expect.poll(() => count(cards)).toBe(2);
		await expect
			.element(
				cards
					.filter({ hasText: "Bump vite" })
					.getByTestId("job-queue-position"),
			)
			.toHaveText("queued · priority #1");
		await expect
			.element(
				cards
					.filter({ hasText: "watch a Project board" })
					.getByTestId("job-kind"),
			)
			.toHaveText("Discuss");
		await filter.getByRole("button", { name: /^Starting/ }).click();

		// Running lists both merges in flight, each merging now (#1433).
		await filter.getByRole("button", { name: /^Running/ }).click();
		await expect.poll(() => count(cards)).toBe(2);
		await expect
			.poll(() => texts(cards.getByTestId("job-queue-position")))
			.toEqual(["merging now", "merging now"]);
	});
}

// Safe merge is queued only from the header's Queue menu, never from a card
// (#1163): a fix job already drives its own pull request to a merge.
for (const [board, path] of [
	["a project board", BOARD],
	["the root board", "/agents/coder/projects"],
]) {
	test(`job and scheduled cards on ${board} offer no safe-merge queue button`, async () => {
		setup();
		await renderApp(path);
		const cards = page.getByTestId("job-card");
		await expect
			.poll(() => count(cards), { timeout: 15_000 })
			.toBe(JOBS.length);
		await expect.poll(() => count(page.getByTestId("scheduled-card"))).toBe(1);
		// The fix job with an open linked pull request, which used to offer it.
		const fix = cards.filter({ hasText: "#218" });
		await expect
			.element(fix.getByTestId("job-pr"))
			.toMatchTextContent("PR #230");
		expect(count(page.getByTestId("job-safe-merge"))).toBe(0);
		expect(count(page.getByRole("button", { name: "Safe merge" }))).toBe(0);

		// The header's Queue menu still opens the safe-merge picker.
		await page.getByTestId("queue-menu").click();
		await page.getByTestId("queue-gh-safe-merge").click();
		const dialog = page.getByTestId("queue-dialog");
		await expect.element(dialog).toHaveAttribute("data-skill", "gh-safe-merge");
		await expect
			.poll(() => count(dialog.getByTestId("queue-candidate")))
			.toBe(4);
		expect(count(dialog.getByTestId("queue-order"))).toBe(0);
	});
}

test("with no_merge, safe merge is disabled with an explanation", async () => {
	setup({ noMerge: true });
	await renderApp(BOARD);
	await page.getByTestId("queue-menu").click();
	const item = page.getByTestId("queue-gh-safe-merge");
	await expect.element(item).toHaveAttribute("data-disabled", "");
	await expect.element(item).toMatchTextContent("no_merge");
	await expect
		.element(item)
		.toHaveAttribute(
			"title",
			expect.stringMatching(
				/no_merge setting leaves pull requests for a human/,
			),
		);
});

test("Discuss queues picked threads and Fix issue queues typed issue numbers", async () => {
	const calls = setup();
	await renderApp(BOARD);
	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-discuss").click();
	const dialog = page.getByTestId("queue-dialog");
	const rows = dialog.getByTestId("queue-candidate");
	await expect
		.element(rows.filter({ hasText: "#44" }).getByTestId("queue-owned"))
		.toBeVisible();
	await rows.filter({ hasText: "#51" }).click();
	await dialog.getByTestId("queue-submit").click();
	await expect
		.poll(() => calls.project_job_queue?.[0])
		.toMatchObject({ skill: "gh-discuss", numbers: [51] });
	await dialog.getByRole("button", { name: "Close" }).click();

	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-fix").click();
	await dialog.getByTestId("queue-issues").fill("#12, 15");
	await expect
		.element(dialog.getByTestId("queue-submit"))
		.toHaveText("Queue 2 issues");
	await dialog.getByTestId("queue-submit").click();
	await expect
		.poll(() => calls.project_job_queue?.[1])
		.toMatchObject({ skill: "gh-fix", numbers: [12, 15] });
});

test("in browser delivery mode the pickers list open PRs and Discussions", async () => {
	const calls = setup({ delivery: "browser" });
	await renderApp(BOARD);
	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-safe-merge").click();
	const dialog = page.getByTestId("queue-dialog");
	const rows = dialog.getByTestId("queue-candidate");
	await expect.poll(() => count(rows)).toBe(4);
	const conflicting = rows.filter({ hasText: "#1212" });
	await expect.element(conflicting).toMatchTextContent("hubot");
	await expect.element(conflicting).toMatchTextContent("pending");
	await expect.element(conflicting).toMatchTextContent("review required");
	await expect.element(conflicting).toMatchTextContent("conflicts");
	await expect
		.element(rows.filter({ hasText: "#1207" }).getByTestId("queue-owned"))
		.toHaveText(`${TARGET}#1207`);
	expect(count(dialog.getByTestId("queue-unavailable"))).toBe(0);
	await commands.keyPress("Escape");

	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-discuss").click();
	await expect.poll(() => count(rows)).toBe(2);
	await expect
		.element(rows.filter({ hasText: "#51" }))
		.toMatchTextContent("Q&A");
	// Each open asks once; the server caches the listing briefly.
	expect(calls.project_queue_candidates).toHaveLength(2);
});

for (const c of [
	{ skill: "gh-safe-merge", noun: "pull requests", number: 1156 },
	{ skill: "gh-discuss", noun: "discussions", number: 44 },
] as const) {
	test(`when ${c.noun} can't be listed, a calm notice shows and numbers still queue`, async () => {
		const calls = setup({ delivery: "browser", listing: "unavailable" });
		await renderApp(BOARD);
		await page.getByTestId("queue-menu").click();
		await page.getByTestId(`queue-${c.skill}`).click();
		const dialog = page.getByTestId("queue-dialog");
		const notice = dialog.getByTestId("queue-unavailable");
		await expect
			.element(notice)
			.toHaveText(
				`Can't list ${c.noun}: sign in to GitHub (gh auth login) to enable this. You can still queue by number.`,
			);
		await expect.element(notice).not.toHaveClass(/red/);
		await expect.element(dialog).not.toMatchTextContent("No open");
		await expect.element(dialog).not.toMatchTextContent("SignInGuard");
		expect(count(dialog.getByRole("alert"))).toBe(0);
		expect(count(dialog.getByTestId("queue-candidates"))).toBe(0);

		const number = dialog.getByTestId("queue-number");
		await number.fill(`#${c.number}`);
		(number.element() as HTMLElement).focus();
		await commands.keyPress("Enter");
		await expect
			.poll(() => calls.project_job_queue?.[0])
			.toEqual({
				target: TARGET,
				skill: c.skill,
				numbers: [c.number],
				titles: {},
			});
		await expect
			.element(dialog.getByTestId("queue-results"))
			.toMatchTextContent(`#${c.number}: queued`);
	});
}

test("a failed listing call shows the notice, not an empty list or a red error", async () => {
	setup({ listing: "error" });
	await renderApp(BOARD);
	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-safe-merge").click();
	const dialog = page.getByTestId("queue-dialog");
	await expect
		.element(dialog.getByTestId("queue-unavailable"))
		.toHaveText(
			"Can't list pull requests right now. You can still queue by number.",
		);
	await expect.element(dialog).not.toMatchTextContent("No open");
	await expect.element(dialog).not.toMatchTextContent("connection refused");
	expect(count(dialog.getByRole("alert"))).toBe(0);
	await expect.element(dialog.getByTestId("queue-number")).toBeEnabled();
});

test("typed PR numbers queue after the picked rows", async () => {
	const calls = setup();
	await renderApp(BOARD);
	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-safe-merge").click();
	const dialog = page.getByTestId("queue-dialog");
	await dialog
		.getByTestId("queue-candidate")
		.filter({ hasText: "#1210" })
		.click();
	await dialog.getByTestId("queue-number").fill("#1300, 1210");
	await expect
		.element(dialog.getByTestId("queue-submit"))
		.toHaveText("Queue 2 PRs");
	await dialog.getByTestId("queue-submit").click();
	await expect
		.poll(() => calls.project_job_queue?.[0]?.numbers)
		.toEqual([1210, 1300]);
});
