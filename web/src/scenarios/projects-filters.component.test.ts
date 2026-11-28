// Ported from web/e2e/projects.spec.ts (#1636): search, state filters and the kanban view.
import { expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, goto, reload, renderApp } from "../component-test/app";
import {
	type Calls,
	chatFixtures,
	inTargets,
	job,
	setup,
	stateChip,
} from "./projects-fixtures";

/** Playwright's toHaveText with a list: one entry per element, in order. */
async function expectTexts(locator: Locator, expected: (string | RegExp)[]) {
	const texts = () =>
		locator
			.elements()
			.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
	await expect
		.poll(texts)
		.toEqual(
			expected.map((e) =>
				typeof e === "string" ? e : expect.stringMatching(e),
			),
		);
}

test("search filters the whole job list across every field", async () => {
	// 30 jobs span three pages. The search must find matches on any page, in
	// the title, issue and PR numbers, session id, and output.
	const many = Array.from({ length: 30 }, (_, i) => {
		const number = 300 + i * 2;
		return job({
			number: number === 350 ? 351 : number,
			title:
				number === 350 ? "Search target on the last page" : `Job ${number}`,
			status: number === 306 ? "failed" : "complete",
			agent: "claude",
			model: "opus",
			started: 10 + i,
			...(number === 310 ? { pr: 351 } : {}),
		});
	});
	const sessions: Record<number, string> = { 320: "sess-351-abc" };
	const logs: Record<number, string> = {
		330: "panic: kaboom",
		306: "retry 351 failed",
	};
	const matches = (j: (typeof many)[number], q: string) =>
		[
			j.key,
			`#${j.number}`,
			j.title,
			j.status,
			j.pull_request ? `#${j.pull_request}` : "",
			sessions[j.number] ?? "",
			logs[j.number] ?? "",
		].some((f) => f.toLowerCase().includes(q.toLowerCase()));
	setup(undefined, {
		project_job_list: (args?: Record<string, unknown>) => {
			const query = typeof args?.query === "string" ? args.query : "";
			const scoped = many.filter(
				(j) =>
					inTargets(args, j.target) &&
					(!args?.status || j.status === args.status) &&
					(!args?.states || (args.states as string[]).includes(j.state)),
			);
			const found = query ? scoped.filter((j) => matches(j, query)) : scoped;
			const offset = Number(args?.offset ?? 0);
			const limit = Number(args?.limit ?? 50);
			return {
				jobs: found.slice(offset, offset + limit),
				total: found.length,
				offset,
				limit,
				...(query ? { unfiltered_total: scoped.length } : {}),
			};
		},
		project_job_get: (args?: Record<string, unknown>) => {
			const found = many.find((j) => j.number === args?.number);
			if (!found) throw new Error("no such job");
			return {
				...found,
				session_id: sessions[found.number] ?? "",
				scrollback: logs[found.number] ?? "",
			};
		},
	});
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const cards = page.getByTestId("job-card");
	const pages = page.getByRole("navigation", { name: "Job pages" });
	const search = page.getByRole("searchbox", { name: "Search jobs" });
	// The search reads as a search box: an icon and the standard field styling.
	await expect
		.element(page.getByTestId("job-toolbar").getByTestId("search-icon"))
		.toBeVisible();
	await expect.element(search).toHaveClass(/\bfield-input\b/);
	await expect.element(pages).toMatchTextContent("Page 1 of 3");
	await expect.poll(() => cards.elements().length).toBe(12);
	await expect
		.poll(() => page.getByTestId("search-count").elements().length)
		.toBe(0);

	// Paging uses chevron icon buttons that still name themselves and disable
	// at either end.
	const prev = pages.getByRole("button", { name: "Previous page" });
	const next = pages.getByRole("button", { name: "Next page" });
	for (const [button, title] of [
		[prev, "Previous page"],
		[next, "Next page"],
	] as const) {
		await expect.element(button).toHaveAttribute("title", title);
		await expect.element(button).toHaveText("");
		await expect.poll(() => button.locator("svg").elements().length).toBe(1);
	}
	await expect.element(prev).toBeDisabled();
	await expect.element(next).toBeEnabled();
	await next.click();
	await next.click();
	await expect.element(pages).toMatchTextContent("Page 3 of 3");
	await expect.element(next).toBeDisabled();
	await expect.element(prev).toBeEnabled();
	await prev.click();

	// Typing filters at once and lands on the first page of the matches.
	await expect.element(pages).toMatchTextContent("Page 2 of 3");
	(search.element() as HTMLElement).focus();
	await commands.keyType("351");
	await expect.element(pages).toMatchTextContent("Page 1 of 1");
	await expect
		.element(page.getByTestId("search-count"))
		.toHaveText("4 of 30 jobs match");
	await expect.poll(() => cards.elements().length).toBe(4);
	for (const n of ["Search target", "Job 310", "Job 320", "Job 306"]) {
		await expect
			.poll(() => cards.filter({ hasText: n }).elements().length)
			.toBe(1);
	}

	// The search narrows within the active status filter.
	await stateChip("Failed").click();
	await expect.poll(() => cards.elements().length).toBe(1);
	await expect.element(cards).toMatchTextContent("#306");
	await expect
		.element(page.getByTestId("search-count"))
		.toHaveText("1 of 1 job matches");
	await stateChip("Failed").click();

	await search.fill("kaboom");
	await expect.poll(() => cards.elements().length).toBe(1);
	await expect.element(cards).toMatchTextContent("#330");
	await search.fill("search target");
	await expect.poll(() => cards.elements().length).toBe(1);
	await expect.element(cards).toMatchTextContent("#351");

	await search.fill("zzzzz");
	await expect.poll(() => cards.elements().length).toBe(0);
	await expect
		.element(page.getByTestId("search-empty"))
		.toMatchTextContent("No jobs match “zzzzz”");
	await expect
		.element(page.getByTestId("search-count"))
		.toHaveText("0 of 30 jobs match");
	await expect.element(pages).toMatchTextContent("Page 1 of 1");

	await page.getByRole("button", { name: "Clear search" }).click();
	await expect.element(search).toHaveValue("");
	await expect.element(pages).toMatchTextContent("Page 1 of 3");
	await expect.poll(() => cards.elements().length).toBe(12);
});

test("the Scheduled and five state filters and kanban toggle sit beside the search", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const toolbar = page.getByTestId("job-toolbar");
	await expect
		.element(toolbar.getByRole("searchbox", { name: "Search jobs" }))
		.toBeVisible();
	await expectTexts(
		toolbar.getByRole("group", { name: "Job filter" }).getByRole("button"),
		[/^Scheduled/, /^Starting/, /^Paused/, /^Running/, /^Done/, /^Failed/],
	);
	await expect.element(toolbar.getByTestId("kanban-toggle")).toBeVisible();
	// The board has no separate Schedules view to switch to (#767).
	await expect
		.poll(
			() =>
				page.getByRole("tablist", { name: "Project views" }).elements().length,
		)
		.toBe(0);
	await expect
		.poll(
			() => page.getByRole("group", { name: "Job filter" }).elements().length,
		)
		.toBe(1);

	// Each state filter shows exactly that state's jobs.
	for (const [tab, numbers] of [
		["Starting", ["#221"]],
		["Running", ["#218"]],
		["Paused", ["#214"]],
		["Done", ["#200"]],
		["Failed", ["#209"]],
	] as const) {
		await stateChip(tab).click();
		const cards = page.getByTestId("job-card");
		await expect.poll(() => cards.elements().length).toBe(numbers.length);
		for (const n of numbers) await expect.element(cards).toMatchTextContent(n);
		await stateChip(tab).click();
	}
});

test("the state chips multi-select and each board remembers its own", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const cards = page.getByTestId("job-card");
	await expect.poll(() => cards.elements().length).toBe(5);
	const total = page.getByTestId("job-total");
	await expect.element(total).toHaveText("5 jobs");
	const pressed = async (on: string[]) => {
		for (const label of ["Starting", "Paused", "Running", "Done", "Failed"])
			await expect
				.element(stateChip(label))
				.toHaveAttribute("aria-pressed", on.includes(label) ? "true" : "false");
	};
	await pressed([]);
	await expect
		.poll(
			() =>
				page.getByRole("button", { name: "Clear", exact: true }).elements()
					.length,
		)
		.toBe(0);

	// Running and Failed together show only their jobs, in list and kanban.
	await stateChip("Running").click();
	await stateChip("Failed").click();
	await pressed(["Running", "Failed"]);
	await expect.element(stateChip("Running")).toHaveClass(/bg-blue-100/);
	await expect.element(stateChip("Done")).not.toHaveClass(/bg-green-100/);
	await expect.poll(() => cards.elements().length).toBe(2);
	await expect
		.poll(() => cards.filter({ hasText: "#218" }).elements().length)
		.toBe(1);
	await expect
		.poll(() => cards.filter({ hasText: "#209" }).elements().length)
		.toBe(1);
	await expect.element(total).toHaveText("2 of 5 jobs");
	await expect.poll(currentURL).toMatch(/[?&]state=running(,|%2C)failed$/);
	await page.getByTestId("kanban-toggle").click();
	const columns = page.getByTestId("kanban-column");
	await expect.poll(() => columns.elements().length).toBe(2);
	await expect.element(columns.nth(0)).toHaveAttribute("data-state", "running");
	await expect.element(columns.nth(1)).toHaveAttribute("data-state", "failed");
	await expect
		.poll(() => page.getByTestId("kanban-card").elements().length)
		.toBe(2);
	await page.getByTestId("kanban-toggle").click();

	// Opening a job's details and going back to the board keeps them.
	await cards
		.filter({ hasText: "#218" })
		.getByRole("link", { name: "Details" })
		.click();
	await expect.element(page.getByTestId("job-detail")).toBeVisible();
	await page.getByTestId("job-detail-close").click();
	await expect.poll(currentURL).toMatch(/[?&]state=running(,|%2C)failed$/);
	await pressed(["Running", "Failed"]);
	await expect.poll(() => cards.elements().length).toBe(2);

	// Another project keeps its own selection, and coming back restores this one.
	await goto("/agents/coder/projects/lsegal%2Fzvidlib");
	await pressed([]);
	await stateChip("Done").click();
	await pressed(["Done"]);
	await goto("/agents/coder/projects/lsegal%2Faviary");
	await pressed(["Running", "Failed"]);
	await expect.poll(() => cards.elements().length).toBe(2);

	// A reload keeps them.
	await reload();
	await pressed(["Running", "Failed"]);
	await expect.poll(() => cards.elements().length).toBe(2);

	// Deselecting every chip shows every job again, and stays that way.
	await page.getByRole("button", { name: "Clear", exact: true }).click();
	await pressed([]);
	await expect.poll(() => cards.elements().length).toBe(5);
	await expect.element(total).toHaveText("5 jobs");
	await expect.poll(currentURL).not.toMatch(/state=/);
	await reload();
	await pressed([]);
	await expect.poll(() => cards.elements().length).toBe(5);
	await stateChip("Done").click();
	await stateChip("Done").click();
	await pressed([]);
	await expect.poll(() => cards.elements().length).toBe(5);
	await goto("/agents/coder/projects/lsegal%2Fzvidlib");
	await pressed(["Done"]);
});

test("a URL's state query preselects its chips over the remembered ones", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await stateChip("Done").click();
	await expect
		.poll(() => page.getByTestId("job-card").elements().length)
		.toBe(1);

	await goto("/agents/coder/projects/lsegal%2Faviary?state=running,failed");
	for (const [label, on] of [
		["Starting", "false"],
		["Paused", "false"],
		["Running", "true"],
		["Done", "false"],
		["Failed", "true"],
	])
		await expect.element(stateChip(label)).toHaveAttribute("aria-pressed", on);
	const cards = page.getByTestId("job-card");
	await expect.poll(() => cards.elements().length).toBe(2);

	// The URL's selection is now the remembered one.
	await goto("/agents/coder/projects/lsegal%2Faviary");
	await expect.poll(currentURL).toMatch(/[?&]state=running(,|%2C)failed$/);
	await expect.poll(() => cards.elements().length).toBe(2);
});

test("a job card's PR icon is colored by its pull request state", async () => {
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	const icon = (n: string) =>
		page
			.getByTestId("job-card")
			.filter({ hasText: n })
			.getByTestId("pr-state-icon");
	for (const [n, label, color] of [
		["#218", "Draft pull request", /text-gray-400/],
		["#209", "Open pull request", /text-green-600/],
		["#200", "Merged pull request", /text-purple-600/],
	] as const) {
		await expect.element(icon(n)).toHaveAttribute("aria-label", label);
		await expect.element(icon(n).locator("title")).toHaveText(label);
		await expect.element(icon(n)).toHaveClass(color);
	}
});

test("the kanban view lays jobs out in state columns of compact cards", async () => {
	const calls = setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await expect
		.poll(() => page.getByTestId("job-card").elements().length)
		.toBe(5);
	const toggle = page.getByTestId("kanban-toggle");
	await toggle.click();
	await expect.element(toggle).toHaveAttribute("aria-pressed", "true");

	const columns = page.getByTestId("kanban-column");
	// A project board leads with its Scheduled column, empty here.
	await expect.poll(() => columns.elements().length).toBe(6);
	await expectTexts(columns.locator("h3"), [
		/Scheduled\s*0/,
		/Starting\s*1/,
		/Paused\s*1/,
		/Running\s*1/,
		/Done\s*1/,
		/Failed\s*1/,
	]);
	await expect
		.poll(() => page.getByTestId("job-card").elements().length)
		.toBe(0);
	const cards = page.getByTestId("kanban-card");
	await expect.poll(() => cards.elements().length).toBe(5);
	// Compact cards: truncated title, PR, timing, and no output.
	const running = columns
		.filter({ hasText: "Running" })
		.getByTestId("kanban-card");
	await expect
		.element(running.locator("a").first())
		.toHaveClass(/\btruncate\b/);
	await expect
		.element(running.getByTestId("job-pr"))
		.toMatchTextContent("PR #233");
	await expect.element(running.getByTestId("kanban-timing")).toHaveText("14m");
	await expect
		.poll(() => page.getByTestId("job-output").elements().length)
		.toBe(0);
	// A paused card says what it waits on, from the job's recorded reason.
	await expect
		.element(
			columns.filter({ hasText: "Paused" }).getByTestId("kanban-paused-reason"),
		)
		.toHaveText("Stacked on PR #226, waiting for it to merge");
	// The kanban reads every job at once rather than a page.
	await expect.poll(() => calls.project_job_list.at(-1)?.limit).toBe(1000);

	// A state filter narrows the board to that state's column.
	await stateChip("Paused").click();
	await expect.poll(() => columns.elements().length).toBe(1);
	await expect.element(columns).toHaveAttribute("data-state", "paused");

	// The layout survives a reload.
	await reload();
	await expect
		.element(page.getByTestId("kanban-toggle"))
		.toHaveAttribute("aria-pressed", "true");
});

test("prompt chat opened mid-turn shows the running turn (#764)", async () => {
	const calls: Calls = {};
	Object.assign(
		calls,
		setup(undefined, {
			...chatFixtures(calls),
			session_turn: (args?: Record<string, unknown>) => ({
				agent_id: args?.agent,
				session_id: args?.session_id,
				processing: true,
				items: [
					{ tool: { id: "call_1", name: "project_status", done: true } },
					{ text: "Reading the board" },
					{ tool: { id: "call_2", name: "project_job_get" } },
				],
			}),
		}),
	);
	await renderApp("/agents/coder/projects/lsegal%2Faviary");
	await page.getByTestId("prompt-chat-toggle").click();
	const chat = page.getByTestId("project-chat");
	await expect
		.poll(() => chat.getByTestId("chat-tool-call").elements().length)
		.toBe(2);
	await expect
		.element(chat.getByTestId("chat-tool-call").nth(1))
		.toHaveAttribute("data-pending", "true");
	// A running call shows on its chip, not in a typing bubble below it.
	await expect
		.poll(() => chat.getByTestId("chat-typing").elements().length)
		.toBe(0);
	await expect.element(chat).toMatchTextContent("Reading the board");
});
