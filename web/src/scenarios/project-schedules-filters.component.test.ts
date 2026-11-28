// Ported from web/e2e/project-schedules.spec.ts (#1636): the agent board's
// scheduled items (#1107) and the Scheduled filter chip with the job state
// chips, on desktop and phone, and the old Schedules URLs.
import { describe, expect, test } from "vitest";
import { commands, page } from "vitest/browser";
import { currentURL, goto, reload, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import {
	AGENT_BOARD,
	BOARD,
	chip,
	closeTaskDialog,
	expectCount,
	expectHidden,
	expectTexts,
	ISSUE_JOB,
	setup,
	shown,
} from "./project-schedules-fixtures";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/ProjectsView.vue"));

const innerText = (el: Element) => (el.textContent ?? "").trim();

describe("the agent board's scheduled items (#1107)", () => {
	test("shows every project's schedules as one card each, with its project", async () => {
		const calls = setup(AGENT_BOARD);
		await renderApp("/agents/coder/projects");

		const cards = page.getByTestId("scheduled-card");
		await expectCount(cards, 3);
		// By next run: zvid-bench in 30m, nightly-triage in 42m, then paused.
		await expectTexts(cards.getByRole("heading"), [
			"zvid-bench",
			"nightly-triage",
			"weekly-deps",
		]);
		await expectTexts(cards.getByTestId("scheduled-project"), [
			"lsegal/zvidlib",
			"lsegal/aviary",
			"lsegal/aviary",
		]);
		const zvid = cards.nth(0);
		const nightly = cards.nth(1);
		const weekly = cards.nth(2);
		await expect
			.element(zvid.getByTestId("scheduled-project"))
			.toHaveAttribute("href", "/agents/coder/projects/lsegal%2Fzvidlib");
		// A run shows in place on its card; no job card copies it.
		await expect
			.element(nightly.getByTestId("scheduled-state"))
			.toHaveText("Running");
		await expect
			.element(nightly.getByTestId("scheduled-running"))
			.toBeVisible();
		await expect
			.element(nightly.getByTestId("scheduled-next"))
			.toMatchTextContent(/in 4[12]m/);
		await expect
			.element(weekly.getByTestId("scheduled-next"))
			.toHaveText("Next run: paused");
		await expect.element(weekly.getByTestId("paused-chip")).toBeVisible();
		await expect
			.element(zvid.getByTestId("scheduled-state"))
			.toHaveText("Scheduled");
		await expect
			.element(zvid.getByTestId("scheduled-next"))
			.toMatchTextContent(/in (29|30)m/);
		await expect
			.element(zvid.getByTestId("scheduled-last-result"))
			.toHaveText("✓");
		await expect
			.element(zvid.getByTestId("scheduled-last-summary"))
			.toHaveText("Benchmarks within noise");
		const jobs = page.getByTestId("job-card");
		await expectCount(jobs, 2);
		await expectTexts(jobs.getByRole("heading"), [
			/Fix the parser/,
			/Decode B-frames/,
		]);
		// The board reads its projects' jobs apart from their schedules'
		// entries, as a project board does.
		expect(calls.project_job_list).toContainEqual(
			expect.objectContaining({
				targets: ["lsegal/aviary", "lsegal/zvidlib"],
				kinds: ["issue", "discussion", "merge", "discuss"],
			}),
		);
		expect(calls.project_job_list).toContainEqual(
			expect.objectContaining({
				targets: ["lsegal/aviary", "lsegal/zvidlib"],
				kinds: ["schedule"],
			}),
		);
		// No Schedule button: a new schedule needs a project, so it is made
		// from a project's board.
		await expectCount(page.getByTestId("schedule-new"), 0);

		// The kanban's Scheduled column names each card's project too, by its
		// short name in the narrow column (#1647).
		await page.getByTestId("kanban-toggle").click();
		const boxes = page.getByTestId("kanban-scheduled-card");
		await expectCount(boxes, 3);
		await expectTexts(boxes.getByTestId("kanban-scheduled-project"), [
			"zvidlib",
			"aviary",
			"aviary",
		]);
		await page.getByTestId("kanban-toggle").click();

		// No pencil (#1165): the gear, the last action, opens the task's full
		// settings in place.
		await expectCount(zvid.getByRole("link", { name: /edit/i }), 0);
		await expect.element(zvid.getByTestId("scheduled-footer")).toBeVisible();
		const gear = [
			...zvid
				.getByTestId("scheduled-footer")
				.element()
				.querySelectorAll<HTMLElement>("a, button"),
		]
			.filter(shown)
			.at(-1) as HTMLElement;
		await expect.element(gear).toHaveAccessibleName("Task settings");
		gear.click();
		const settings = page.getByRole("dialog", { name: "Edit task" });
		await expect
			.element(settings.getByLabelText("Task name"))
			.toHaveValue("zvid-bench");
		await expect
			.poll(currentURL)
			.toMatch(/^[^?]*\/agents\/coder\/projects(\?|$)/);
		await closeTaskDialog();
	});

	test("the Scheduled chip filters schedules with the other chips and persists", async () => {
		setup(AGENT_BOARD);
		await renderApp("/agents/coder/projects");
		const scheduled = page.getByTestId("scheduled-card");
		const jobs = page.getByTestId("job-card");
		const total = page.getByTestId("job-total");

		// No chip shows everything.
		await expect.element(chip("Scheduled")).toHaveText(/^Scheduled\s*3$/);
		await expectCount(scheduled, 3);
		await expectCount(jobs, 2);
		await expect.element(total).toHaveText("5 items");

		// Scheduled alone shows only the schedules.
		await chip("Scheduled").click();
		await expect.poll(currentURL).toMatch(/[?&]state=scheduled$/);
		await expectCount(scheduled, 3);
		await expectCount(jobs, 0);

		// With Failed it shows the union.
		await chip("Failed").click();
		await expect.poll(currentURL).toMatch(/[?&]state=scheduled(,|%2C)failed$/);
		await expectCount(scheduled, 3);
		await expectCount(jobs, 1);
		await expect
			.element(jobs.getByRole("heading"))
			.toMatchTextContent("Decode B-frames");
		expect(localStorage.getItem("aviary.projects.states.agent:coder")).toBe(
			"scheduled,failed",
		);

		// A reload, and leaving the board and coming back, keep it.
		await reload();
		await expect
			.element(chip("Scheduled"))
			.toHaveAttribute("aria-pressed", "true");
		await expect
			.element(chip("Failed"))
			.toHaveAttribute("aria-pressed", "true");
		await expectCount(scheduled, 3);
		await goto("/agents/coder/projects/lsegal%2Fzvidlib");
		await expectCount(page.getByTestId("scheduled-card"), 1);
		await goto("/agents/coder/projects");
		await expect.poll(currentURL).toMatch(/[?&]state=scheduled(,|%2C)failed$/);
		await expect
			.element(chip("Scheduled"))
			.toHaveAttribute("aria-pressed", "true");
		await expectCount(scheduled, 3);
		await expectCount(jobs, 1);

		// Deselecting Scheduled while Failed stays selected hides them: no
		// schedule's last run failed.
		await chip("Scheduled").click();
		await expect.poll(currentURL).toMatch(/[?&]state=failed$/);
		await expectCount(scheduled, 0);
		await expectCount(jobs, 1);
	});

	test("its counts are the sums of the project boards' counts", async () => {
		setup(AGENT_BOARD);
		const labels = [
			"Scheduled",
			"Starting",
			"Paused",
			"Running",
			"Done",
			"Failed",
		];
		let mounted = false;
		async function counts(path: string) {
			if (mounted) await goto(path);
			else await renderApp(path);
			mounted = true;
			await expect.element(page.getByTestId("stat-card").first()).toBeVisible();
			await expect
				.element(page.getByTestId("scheduled-card").first())
				.toBeVisible();
			const out: Record<string, number> = {};
			for (const label of labels) {
				const text = innerText(chip(label).element()).trim();
				out[`chip ${label}`] = Number(text.replace(/^\D+/, ""));
			}
			for (const card of page.getByTestId("stat-card").all()) {
				const label = innerText(card.getByTestId("stat-label").element());
				if (!labels.some((l) => l.toUpperCase() === label.toUpperCase()))
					continue;
				out[`stat ${label.toLowerCase()}`] = Number(
					innerText(card.getByTestId("stat-value").element()),
				);
			}
			out.total = Number(
				innerText(page.getByTestId("job-total").element()).replace(/\D+$/, ""),
			);
			return out;
		}
		const aviary = await counts("/agents/coder/projects/lsegal%2Faviary");
		const zvid = await counts("/agents/coder/projects/lsegal%2Fzvidlib");
		const root = await counts("/agents/coder/projects");
		const sums = Object.fromEntries(
			Object.keys(aviary).map((k) => [k, aviary[k] + zvid[k]]),
		);
		expect(root).toEqual(sums);
		// Each schedule counts once under Scheduled, the running one counts
		// as Running, and finished runs leave Done alone.
		expect(root["chip Scheduled"]).toBe(3);
		expect(root["chip Running"]).toBe(2);
		expect(root["chip Done"]).toBe(4);
	});
});

test("the Scheduled chip counts, filters and persists with the job state chips", async () => {
	setup({ jobs: [ISSUE_JOB] });
	await renderApp(BOARD);
	const scheduled = page.getByTestId("scheduled-card");
	const jobs = page.getByTestId("job-card");
	const total = page.getByTestId("job-total");

	// No chip selected shows the scheduled items and every job, and the
	// total counts both: one job and two scheduled items.
	await expect.element(chip("Scheduled")).toHaveText(/^Scheduled\s*2$/);
	await expect
		.element(chip("Scheduled"))
		.toHaveAttribute("aria-pressed", "false");
	await expectCount(scheduled, 2);
	await expectCount(jobs, 1);
	await expect.element(total).toHaveText("3 items");

	// Scheduled alone shows only the scheduled items.
	await chip("Scheduled").click();
	await expect.poll(currentURL).toMatch(/[?&]state=scheduled$/);
	await expect.element(chip("Scheduled")).toHaveClass(/bg-violet-100/);
	await expectCount(scheduled, 2);
	await expectCount(jobs, 0);
	await expect.element(total).toHaveText(/^2 of \d+ items$/);

	// With Running it shows both.
	await chip("Running").click();
	await expect.poll(currentURL).toMatch(/[?&]state=scheduled(,|%2C)running$/);
	await expectCount(scheduled, 2);
	await expectCount(jobs, 1);
	await expect.element(total).toHaveText(/^3 of \d+ items$/);

	// A reload keeps the selection.
	await reload();
	await expect
		.element(chip("Scheduled"))
		.toHaveAttribute("aria-pressed", "true");
	await expect.element(chip("Running")).toHaveAttribute("aria-pressed", "true");
	await expectCount(scheduled, 2);
	await expectCount(jobs, 1);

	// The kanban follows the chips: a Scheduled column and a Running one.
	await page.getByTestId("kanban-toggle").click();
	const columns = page.getByTestId("kanban-column");
	await expectCount(columns, 2);
	await expect
		.element(columns.nth(0))
		.toHaveAttribute("data-state", "scheduled");
	await expect.element(columns.nth(1)).toHaveAttribute("data-state", "running");
	await page.getByTestId("kanban-toggle").click();

	// Deselecting Scheduled while Running stays selected leaves the running
	// schedule on its own card, never a job card of its run (#977).
	await chip("Scheduled").click();
	await expect.poll(currentURL).toMatch(/[?&]state=running$/);
	await expectCount(scheduled, 1);
	await expect
		.element(scheduled.getByRole("heading"))
		.toHaveText("nightly-triage");
	await expectCount(jobs, 1);
	await expect
		.element(jobs.getByRole("heading"))
		.toMatchTextContent("Fix the parser");
});

test("a mobile board folds the filter chips into a Filter button", async () => {
	await page.viewport(375, 812);
	setup({ jobs: [ISSUE_JOB] });
	await renderApp(BOARD);
	const toolbar = page.getByTestId("job-toolbar");
	const toggle = page.getByTestId("filter-toggle");
	const group = page.getByRole("group", { name: "Job filter" });
	const searchbox = toolbar.getByRole("searchbox", { name: "Search jobs" });

	// Collapsed: search, one Filter button, the layout toggle and the count.
	await expect.element(searchbox).toBeVisible();
	await expect.element(toggle).toBeVisible();
	await expect.element(toggle).toHaveAttribute("aria-expanded", "false");
	await expect.element(toggle).toHaveText("Filters");
	await expectHidden(group);
	await expect.element(page.getByTestId("kanban-toggle")).toBeVisible();
	await expect.element(page.getByTestId("job-total")).toHaveText("3 items");
	// The search keeps a usable width; Filter, layout toggle and count share a row.
	expect(searchbox.element().getBoundingClientRect().width).toBeGreaterThan(
		200,
	);
	const rowOf = (testId: string) => {
		const box = page.getByTestId(testId).element().getBoundingClientRect();
		return Math.round(box.y + box.height / 2);
	};
	const filterRow = rowOf("filter-toggle");
	expect(Math.abs(rowOf("kanban-toggle") - filterRow)).toBeLessThan(6);
	expect(Math.abs(rowOf("job-total") - filterRow)).toBeLessThan(6);

	// Opening lists the same chips and counts; toggling works as inline.
	await toggle.click();
	await expect.element(toggle).toHaveAttribute("aria-expanded", "true");
	await expect.element(group).toBeVisible();
	await expect.element(chip("Scheduled")).toHaveText(/^Scheduled\s*2$/);
	await chip("Running").click();
	await expect.poll(currentURL).toMatch(/[?&]state=running$/);
	await expect.element(chip("Running")).toHaveAttribute("aria-pressed", "true");
	await chip("Scheduled").click();
	await expect.element(toggle).toHaveText("Filters · 2");

	// Esc closes the panel and returns focus to the button.
	await commands.keyPress("Escape");
	await expectHidden(group);
	await expect.element(toggle).toHaveFocus();
	await expect.element(toggle).toHaveAttribute("aria-expanded", "false");

	// The keyboard opens it again; Clear empties the selection.
	await commands.keyPress("Enter");
	await expect.element(group).toBeVisible();
	await group.getByRole("button", { name: "Clear" }).click();
	await expect.poll(currentURL).not.toMatch(/state=/);
	await expect.element(toggle).toHaveText("Filters");

	// A tap outside closes it.
	await page.getByTestId("job-total").click();
	await expectHidden(group);

	// The selection persists across a reload.
	await toggle.click();
	await chip("Running").click();
	await reload();
	await expect.element(toggle).toHaveText("Filters · 1");

	// Desktop widths show the inline chips and no Filter button.
	await page.viewport(1280, 720);
	await expectHidden(toggle);
	await expect.element(group).toBeVisible();
	await expect.element(chip("Running")).toHaveAttribute("aria-pressed", "true");
});

test("a mobile agent board's Filter panel has a Scheduled option (#1107)", async () => {
	await page.viewport(375, 812);
	setup({ owner: "coder" });
	await renderApp("/agents/coder/projects");
	await page.getByTestId("filter-toggle").click();
	const group = page.getByRole("group", { name: "Job filter" });
	await expect
		.element(group.getByRole("button", { name: /^Running/ }))
		.toBeVisible();
	await expect
		.element(group.getByRole("button", { name: /^Scheduled/ }))
		.toHaveText(/^Scheduled\s*2$/);
});

for (const query of ["tab=schedules", "view=schedules"])
	test(`the old Schedules URL ?${query} lands on the board with only Scheduled selected`, async () => {
		setup();
		await renderApp(`${BOARD}?${query}`);
		await expect.poll(currentURL).toMatch(/\?state=scheduled$/);
		for (const label of [
			"Scheduled",
			"Starting",
			"Paused",
			"Running",
			"Done",
			"Failed",
		])
			await expect
				.element(chip(label))
				.toHaveAttribute(
					"aria-pressed",
					label === "Scheduled" ? "true" : "false",
				);
		await expectCount(page.getByTestId("scheduled-card"), 2);
		await expectCount(page.getByTestId("job-card"), 0);
	});

test("an old Schedules URL for one schedule lands on the board's scheduled items", async () => {
	setup();
	await renderApp(`${BOARD}?tab=schedules&schedule=assistant/nightly-triage`);
	await expect.poll(currentURL).toMatch(/\?state=scheduled$/);
	await expectCount(page.getByTestId("scheduled-card"), 2);
});
