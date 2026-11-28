// Ported from web/e2e/projects.spec.ts (#1636): the rich transcript at narrow widths and the board header layout.
import { beforeEach, expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, goto, reload, renderApp } from "../component-test/app";
import { expectHeaderOneLine, headerSettled } from "./helpers/boardHeader";
import { boundingBoxOf } from "./helpers/box";
import { at, CLONES, EVENTS, setup, stateChip } from "./projects-fixtures";

// A fresh page starts unscrolled; earlier tests here scroll the document.
beforeEach(() => {
	window.scrollTo(0, 0);
});

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

// Long unbroken tokens: a clone path, a URL and minified JSON.
const LONG_PATH =
	"/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-761-b/internal/projects/core/very_long_directory_name_without_breaks/file_with_a_really_long_name.go";
const LONG_URL =
	"https://github.com/lsegal/aviary/actions/runs/43097434009/job/123456789012?pr=834&check_suite_focus=true&very_long_query_parameter=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const LONG_JSON = JSON.stringify({
	type: "assistant",
	message: { content: [{ type: "text", text: "x".repeat(160) }] },
});
const LONG_EVENTS = [
	{ kind: "system", time: at(9), text: `Cloned into ${LONG_PATH}` },
	{
		kind: "message",
		time: at(8),
		text: `See ${LONG_URL} and \`${LONG_PATH}\`.\n\n\`\`\`json\n${LONG_JSON}\n\`\`\``,
	},
	{ kind: "reasoning", time: at(8), text: LONG_JSON },
	{
		kind: "plan",
		time: at(7),
		items: [{ text: `Edit ${LONG_PATH}`, status: "in_progress" }],
	},
	{
		kind: "tool_call",
		time: at(7),
		call_id: "w1",
		name: "Read",
		input: { file_path: LONG_PATH },
	},
	{ kind: "tool_result", time: at(7), call_id: "w1", output: LONG_JSON },
	{
		kind: "file_change",
		time: at(6),
		path: LONG_PATH,
		diff: `@@ -1,1 +1,1 @@\n-${LONG_JSON}\n+${LONG_URL}`,
	},
	{
		kind: "command",
		time: at(5),
		call_id: "w2",
		command: `cat ${LONG_PATH} | curl -d @- ${LONG_URL}`,
		status: "running",
	},
	{
		kind: "command",
		time: at(5),
		call_id: "w2",
		output: `${LONG_JSON}\n${LONG_URL}`,
		exit_code: 0,
		status: "done",
	},
	{
		kind: "comment_relay",
		time: at(4),
		author: "lsegal",
		text: `Look at ${LONG_URL}`,
		url: "https://github.com/lsegal/aviary/issues/221#issuecomment-1",
	},
];

// Elements wider than their box, other than one-line rows that truncate
// with an ellipsis on purpose.
const sidewaysOverflow = (root: Element) =>
	[root, ...root.querySelectorAll("*")]
		.filter(
			(el): el is HTMLElement =>
				el instanceof HTMLElement &&
				getComputedStyle(el).textOverflow !== "ellipsis" &&
				!el.classList.contains("sr-only") &&
				el.scrollWidth > el.clientWidth + 1,
		)
		.map((el) => `${el.tagName}.${el.className}`);

for (const { name, width, height } of [
	{ name: "mobile", width: 390, height: 844 },
	{ name: "narrow desktop", width: 820, height: 800 },
]) {
	test(`the rich transcript never scrolls sideways at ${name} width (#834)`, async () => {
		EVENTS[221] = LONG_EVENTS;
		const clone = CLONES[221];
		CLONES[221] = { checkout_directory: LONG_PATH, checkout_state: "present" };
		try {
			await page.viewport(width, height);
			setup();
			await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/221");
			const transcript = page.getByTestId("job-transcript");
			await expect
				.poll(
					() => transcript.locator('[data-event="command"]').elements().length,
				)
				.toBe(1);
			// Expanded content wraps too.
			await ((el) => {
				for (const d of el.querySelectorAll("details")) d.open = true;
			})(transcript.element() as HTMLElement);
			expect(
				await sidewaysOverflow(transcript.element() as HTMLElement),
			).toEqual([]);
			expect(
				await sidewaysOverflow(
					page.getByTestId("job-meta").element() as HTMLElement,
				),
			).toEqual([]);
			await expect
				.element(page.getByTestId("job-clone-path"))
				.toHaveText(LONG_PATH);

			// The board card's transcript shares the rule.
			await goto("/agents/coder/projects/lsegal%2Faviary");
			const rich = page
				.getByTestId("job-card")
				.filter({ hasText: "#221" })
				.getByTestId("job-output-rich");
			await expect.element(rich.locator('[data-row="message"]')).toBeVisible();
			expect(await sidewaysOverflow(rich.element() as HTMLElement)).toEqual([]);
		} finally {
			delete EVENTS[221];
			CLONES[221] = clone;
		}
	});
}

test("at 390px the Code Loop header is one chip row and the top nav hides its scrollbar (#1209, #1570)", async () => {
	await page.viewport(390, 844);
	setup();
	await renderApp("/agents/coder/projects");
	// Measure once the phone's project row has loaded above the header.
	await expect
		.element(page.getByTestId("mobile-sub-nav").getByText("lsegal/zvidlib"))
		.toBeVisible();
	// The breadcrumb names the page, so the title is only for screen readers.
	const title = page.getByTestId("settings-title");
	await expect.element(title).toHaveText("Code Loop");
	expect(
		await ((el) => el.getBoundingClientRect().width)(
			title.element() as HTMLElement,
		),
	).toBe(1);
	await expect
		.element(page.getByTestId("settings-current"))
		.toHaveText("Code Loop");
	await expect
		.poll(() => page.getByTestId("settings-header-icon").elements().length)
		.toBe(0);

	// The chips and the two icons share one row inside the screen.
	const ids = [
		"board-projects",
		"delivery-status",
		"instance-tag",
		"board-info",
	];
	const boxes: DOMRect[] = [];
	for (const id of ids) {
		const box = await boundingBoxOf(page.getByTestId(id));
		expect(box, id).not.toBeNull();
		expect((box?.x ?? 0) + (box?.width ?? 0), id).toBeLessThanOrEqual(390);
		boxes.push(box);
	}
	const mid = (b: (typeof boxes)[number]) => (b?.y ?? 0) + (b?.height ?? 0) / 2;
	for (const b of boxes)
		expect(Math.abs(mid(b) - mid(boxes[0]))).toBeLessThan(4);
	// The chips and the toolbar share one line (#1576).
	await headerSettled();
	await expectHeaderOneLine();

	// Nothing in the header is wider than the screen, so nothing scrolls.
	const header = await ((root) => ({
		wide: [root, ...root.querySelectorAll("*")]
			.filter((el) => el.getBoundingClientRect().right > window.innerWidth + 1)
			.map((el) => el.outerHTML.slice(0, 80)),
		scrollers: [root, ...root.querySelectorAll("*")]
			.filter((el) => el.scrollWidth > el.clientWidth + 1)
			.filter((el) => getComputedStyle(el).overflowX !== "hidden")
			.map((el) => el.outerHTML.slice(0, 80)),
	}))(page.getByTestId("settings-header").element() as HTMLElement);
	expect(header).toEqual({ wide: [], scrollers: [] });
	expect(
		await (() => document.documentElement.scrollWidth <= window.innerWidth)(),
	).toBe(true);

	// The top nav and the chip rows swipe without a visible scrollbar.
	for (const id of ["mobile-main-nav", "project-nav", "stat-row"]) {
		const row = page.getByTestId(id);
		if (!(await row.elements().length)) continue;
		expect(
			await ((el: HTMLElement) => ({
				width: getComputedStyle(el).scrollbarWidth,
				bar: el.offsetHeight - el.clientHeight,
			}))(row.element() as HTMLElement),
			id,
		).toEqual({ width: "none", bar: 0 });
	}
	const nav = page.getByTestId("mobile-main-nav");
	const navLayout = await ((el) => {
		const box = el.getBoundingClientRect();
		const active = el.querySelector("[data-active]")?.getBoundingClientRect();
		return {
			overflowX: getComputedStyle(el).overflowX,
			scrolls: el.scrollWidth > el.clientWidth,
			activeInView:
				!!active &&
				active.left >= box.left - 1 &&
				active.right <= box.right + 1,
		};
	})(nav.element() as HTMLElement);
	expect(navLayout.overflowX).toBe("auto");
	expect(navLayout.activeInView).toBe(true);
	if (navLayout.scrolls) {
		await ((el) => {
			el.scrollLeft = 0;
		})(nav.element() as HTMLElement);
		await expect
			.poll(() =>
				((el) => getComputedStyle(el).maskImage)(nav.element() as HTMLElement),
			)
			.toMatch(/rgba\(0, 0, 0, 0\)\)$/);
	}
});

for (const [label, width] of [
	["mobile", 360],
	["mobile", 390],
	["mobile", 430],
	["desktop", 1280],
] as const) {
	test(`the Schedule button is icon-only on phones and labeled on desktop at ${width}px (#1374)`, async () => {
		await page.viewport(width, 844);
		setup();
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		const schedule = page.getByTestId("schedule-new");
		await headerSettled();
		if (label === "mobile" && !isVisible(schedule)) {
			// On a narrow phone it moved into "…" to keep the header on one
			// line (#1576).
			await page.getByTestId("board-more-menu").click();
			await expect
				.element(page.getByRole("menuitem", { name: "Schedule task" }))
				.toBeVisible();
			return;
		}
		await expect.element(schedule).toBeVisible();
		await expect(schedule).toHaveAccessibleName("Schedule task");
		await expect.element(schedule).toHaveAttribute("title", "Schedule task");
		const text = schedule.getByText("Schedule", { exact: true });
		const check = page.getByTestId("board-more-menu");
		const s = await boundingBoxOf(schedule);
		const c = await boundingBoxOf(check);
		if (label === "mobile") {
			// The same square as the icon-only "…" button beside it.
			await expectHidden(text);
			expect(Math.abs((s?.width ?? 0) - (c?.width ?? 0))).toBeLessThanOrEqual(
				1,
			);
			expect(Math.abs((s?.height ?? 0) - (c?.height ?? 0))).toBeLessThanOrEqual(
				1,
			);
			// Nothing scrolls sideways (#997, #1209).
			expect(
				await (() =>
					document.documentElement.scrollWidth <= window.innerWidth)(),
			).toBe(true);
			expect((s?.x ?? 0) + (s?.width ?? 0)).toBeLessThanOrEqual(width);
		} else {
			await expect.element(text).toBeVisible();
			expect(s?.width ?? 0).toBeGreaterThan((c?.width ?? 0) + 20);
		}
	});
}

// The compact Code Loop header is slim enough to stay pinned whole under
// the breadcrumb bar, so its chips and toolbar stay in reach (#1572).
for (const path of [
	"/agents/coder/projects",
	"/agents/coder/projects/lsegal%2Faviary",
]) {
	test(`the compact header stays pinned under the breadcrumb while ${path} scrolls (#1572)`, async () => {
		await page.viewport(390, 700);
		setup();
		await renderApp(path);
		const crumbs = page.getByTestId("settings-crumb-bar");
		const titleRow = page.getByTestId("settings-title-row");
		const body = page.getByTestId("page-body");
		await expect
			.element(page.getByTestId("delivery-status"))
			.toHaveText("live");
		const content = page.getByRole("region", { name: "Job counts" });
		await expect.element(content).toBeVisible();
		const bar = await boundingBoxOf(crumbs);
		const row = await boundingBoxOf(titleRow);
		if (!bar || !row) throw new Error("no header");
		// The header sits right under the breadcrumb bar, and the two stay
		// slim: about two rows of controls.
		expect(row.y).toBeCloseTo(bar.y + bar.height, 0);
		expect(row.y + row.height - bar.y).toBeLessThanOrEqual(130);
		const top = async (l: Locator) => (await boundingBoxOf(l))?.y ?? 0;
		const contentBefore = await top(content);
		expect(await top(body)).toBeCloseTo(row.y + row.height, 0);

		// Scrolling the board moves only the board.
		await content.hover();
		for (let i = 0; i < 6; i++) await commands.mouseWheel(0, 100);
		await expect
			.poll(async () => contentBefore - (await top(content)))
			.toBeGreaterThan(30);
		expect(await top(crumbs)).toBeCloseTo(bar.y, 0);
		expect(await top(titleRow)).toBeCloseTo(row.y, 0);
		expect(await top(body)).toBeCloseTo(row.y + row.height, 0);
		await expect.element(page.getByTestId("board-more-menu")).toBeInViewport();
		await expect.element(page.getByTestId("delivery-status")).toBeInViewport();

		// A wheel over the header itself doesn't move it either.
		await crumbs.hover();
		for (let i = 0; i < 4; i++) await commands.mouseWheel(0, 100);
		await new Promise((r) => setTimeout(r, 100));
		expect(await top(titleRow)).toBeCloseTo(row.y, 0);
	});
}

test("a job's full header still slides away under the breadcrumb bar (#1437)", async () => {
	await page.viewport(390, 700);
	setup();
	await renderApp("/agents/coder/projects/lsegal%2Faviary/jobs/218");
	const crumbs = page.getByTestId("settings-crumb-bar");
	const titleRow = page.getByTestId("settings-title-row");
	await expect.element(page.getByTestId("job-detail-title")).toBeVisible();
	// Measure once the phone's project row has loaded above the header.
	await expect
		.element(page.getByTestId("mobile-sub-nav").getByText("lsegal/zvidlib"))
		.toBeVisible();
	const bar = await boundingBoxOf(crumbs);
	const row = await boundingBoxOf(titleRow);
	if (!bar || !row) throw new Error("no header");
	await crumbs.hover();
	for (let i = 0; i < 6; i++) await commands.mouseWheel(0, 100);
	await expect
		.poll(async () => (await boundingBoxOf(titleRow))?.y)
		.toBeLessThan(row.y - 10);
	expect((await boundingBoxOf(crumbs))?.y).toBeCloseTo(bar.y, 0);
	// Tabbing to one of its actions brings the row back.
	(titleRow.getByRole("button").first().element() as HTMLElement).focus();
	await expect
		.poll(async () => (await boundingBoxOf(titleRow))?.y)
		.toBeCloseTo(row.y, 0);
});

// The filter row keeps search usable at every width: the state filters
// show as one segmented control when they fit beside it and fold into a
// "Filters" button otherwise, decided by the toolbar's own width (#1490).
for (const { width, chat } of [
	{ width: 1600, chat: false },
	{ width: 1345, chat: false },
	{ width: 1345, chat: true },
	{ width: 1024, chat: false },
	{ width: 768, chat: false },
	{ width: 390, chat: false },
])
	test(`the filter row at ${width}px${chat ? " with the chat panel" : ""} keeps search usable (#1490)`, async () => {
		await page.viewport(width, 900);
		setup();
		await renderApp("/agents/coder/projects/lsegal%2Faviary");
		const toolbar = page.getByTestId("job-toolbar");
		const search = toolbar.getByRole("searchbox", { name: "Search jobs" });
		await expect.element(search, { timeout: 15_000 }).toBeVisible();
		if (chat) await page.getByTestId("prompt-chat-toggle").click();
		const segments = page.getByTestId("state-filter-segments");
		const toggle = page.getByTestId("filter-toggle");
		await expect.element(segments.or(toggle)).toBeVisible();
		const rowBox = await boundingBoxOf(toolbar);
		const searchBox = await boundingBoxOf(search);
		if (width < 640) {
			// A phone's search takes its own full-width row.
			expect(searchBox?.width ?? 0).toBeGreaterThan((rowBox?.width ?? 0) - 2);
			await expect.element(toggle).toBeVisible();
		} else {
			expect(searchBox?.width ?? 0).toBeGreaterThanOrEqual(220);
			expect(searchBox?.width ?? 0).toBeLessThanOrEqual(320);
		}
		if (width === 1345 && !chat) await expect.element(segments).toBeVisible();
		if (chat) await expect.element(toggle).toBeVisible();

		// Search, the filters, the layout toggle and the count share a line
		// (below search on a phone), with nothing past the row's edge.
		const mid = async (l: Locator) => {
			const b = await boundingBoxOf(l);
			return (b?.y ?? 0) + (b?.height ?? 0) / 2;
		};
		const filterControl = isVisible(segments) ? segments : toggle;
		const line = await mid(filterControl);
		expect(
			Math.abs((await mid(page.getByTestId("kanban-toggle"))) - line),
		).toBeLessThan(4);
		expect(
			Math.abs((await mid(page.getByTestId("job-total"))) - line),
		).toBeLessThan(4);
		if (width >= 640)
			expect(Math.abs((await mid(search)) - line)).toBeLessThan(4);
		const overflow = await ((row) => {
			const right = row.getBoundingClientRect().right;
			return {
				row: row.scrollWidth - row.clientWidth,
				page: document.documentElement.scrollWidth - window.innerWidth,
				past: [...row.children].some(
					(c) => c.getBoundingClientRect().right > right + 1,
				),
			};
		})(toolbar.element() as HTMLElement);
		expect(overflow).toEqual({ row: 0, page: 0, past: false });

		// Either form lists the same options and counts and toggles alike.
		const folded = isVisible(toggle);
		if (folded) await toggle.click();
		const group = page.getByRole("group", { name: "Job filter" });
		await expectTexts(group.getByRole("button", { name: /^(?!Clear)/ }), [
			/^Scheduled/,
			/^Starting/,
			/^Paused/,
			/^Running\s*1$/,
			/^Done/,
			/^Failed/,
		]);
		await stateChip("Running").click();
		await stateChip("Done").click();
		await expect
			.poll(currentURL)
			.toMatch(/[?&]state=running%2Cdone|state=running,done/);
		await expect
			.element(stateChip("Running"))
			.toHaveAttribute("aria-pressed", "true");
		if (folded) await expect.element(toggle).toHaveText("Filters · 2");
		await reload();
		if (folded) {
			await expect.element(toggle).toHaveText("Filters · 2");
			await toggle.click();
		}
		await expect
			.element(stateChip("Done"))
			.toHaveAttribute("aria-pressed", "true");
		await toolbar.getByRole("button", { name: "Clear" }).click();
		await expect.poll(currentURL).not.toMatch(/state=/);
	});
