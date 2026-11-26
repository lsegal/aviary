import { expect, type Page, test } from "@playwright/test";
import { A, mockAviary, PAGES } from "./helpers/pageHeaderPages";

// Every page header's actions sit on one row (#1347): each button, menu and
// select shares the row's vertical center and height, at desktop, tablet and
// phone widths, and nothing in the header runs past the screen. On the
// channel page the conversation picker and the gear used to wrap into two
// ragged lines.

const WIDTHS = [1280, 768, 390];

const NOW = "2026-11-26T08:00:00Z";

/** The channel has its own conversation and one it received, so its page shows the picker. */
const CHANNEL_SESSIONS = [
	{
		id: "channel:signal:signal",
		agent_id: "Aviary",
		name: "channel:signal:signal",
		created_at: NOW,
		own: true,
	},
	{
		id: "channel:signal:+15550100",
		agent_id: "Aviary",
		name: "channel:signal:+15550100",
		title: "A conversation with a long title that has to truncate",
		created_at: NOW,
	},
];

/** The header's actions and how they sit in it. */
async function headerLayout(page: Page) {
	return page.getByTestId("settings-header").evaluate((header) => {
		const actions = header.querySelector('[data-testid="settings-actions"]');
		const row = header.querySelector('[data-testid="settings-title-row"]');
		const title = header.querySelector('[data-testid="settings-title"]');
		if (!actions || !row || !title) throw new Error("no header row");
		// The outermost visible controls: a menu's trigger, not the icon inside
		// it, and not a select's visually hidden native twin.
		const sel =
			"button, a[href], select, input, textarea, [role=combobox], [role=button]";
		const controls = [...actions.querySelectorAll<HTMLElement>(sel)]
			.filter(
				(el) =>
					!el.parentElement?.closest(sel) &&
					!el.closest('[aria-hidden="true"]'),
			)
			.map((el) => ({ el, box: el.getBoundingClientRect() }))
			.filter(({ box }) => box.width > 1 && box.height > 1)
			.map(({ el, box }) => ({
				name:
					el.getAttribute("data-testid") ||
					el.getAttribute("aria-label") ||
					el.textContent?.trim() ||
					el.tagName,
				center: box.top + box.height / 2,
				height: box.height,
			}));
		const a = actions.getBoundingClientRect();
		const r = row.getBoundingClientRect();
		const titleBlock = title.closest(".flex-1")?.getBoundingClientRect();
		return {
			controls,
			actionsRight: a.right,
			actionsCenter: a.top + a.height / 2,
			rowRight: r.right - Number.parseFloat(getComputedStyle(row).paddingRight),
			// Actions beside the title rather than moved under it.
			besideTitle: !!titleBlock && a.top < titleBlock.bottom,
			titleCenter: titleBlock ? titleBlock.top + titleBlock.height / 2 : 0,
			headerOverflow: header.scrollWidth - header.clientWidth,
			pastScreen: [header, ...header.querySelectorAll("*")]
				.filter(
					(el) =>
						el.getBoundingClientRect().right > window.innerWidth + 1 &&
						getComputedStyle(el).display !== "none",
				)
				.map((el) => el.getAttribute("data-testid") || el.tagName),
			pageOverflow:
				document.documentElement.scrollWidth -
				document.documentElement.clientWidth,
		};
	});
}

test.beforeEach(async ({ page }) => {
	await mockAviary(page, { channel_sessions: CHANNEL_SESSIONS });
});

for (const width of WIDTHS) {
	for (const p of PAGES) {
		test(`at ${width}px ${p.url} keeps its header actions on one row`, async ({
			page,
		}) => {
			await page.setViewportSize({ width, height: 800 });
			await page.goto(p.url);
			await expect(page.getByTestId("settings-title")).toHaveText(p.title);
			if (p.url === `${A}/channels/signal`)
				await expect(page.getByTestId("channel-session-picker")).toBeVisible();
			const layout = await headerLayout(page);
			const [first] = layout.controls;
			for (const c of layout.controls) {
				expect(
					Math.abs(c.center - first.center),
					`${c.name} is off ${first.name}'s row`,
				).toBeLessThanOrEqual(2);
				expect(
					Math.abs(c.height - first.height),
					`${c.name} is not as tall as ${first.name}`,
				).toBeLessThanOrEqual(1);
			}
			if (first) {
				// Right-aligned, and centered with the title when beside it.
				expect(
					Math.abs(layout.actionsRight - layout.rowRight),
				).toBeLessThanOrEqual(1);
				if (layout.besideTitle)
					expect(
						Math.abs(layout.actionsCenter - layout.titleCenter),
					).toBeLessThanOrEqual(2);
			}
			expect(layout.headerOverflow).toBeLessThanOrEqual(0);
			expect(layout.pastScreen).toEqual([]);
			expect(layout.pageOverflow).toBeLessThanOrEqual(0);
		});
	}
}

test("the channel page's picker and gear share one row beside the title", async ({
	page,
}) => {
	for (const width of [1280, 768]) {
		await page.setViewportSize({ width, height: 800 });
		await page.goto(`${A}/channels/signal`);
		const picker = page.getByTestId("channel-session-picker");
		const gear = page.getByTestId("channel-settings-link");
		await expect(picker).toBeVisible();
		const p = await picker.boundingBox();
		const g = await gear.boundingBox();
		if (!p || !g) throw new Error("no picker or gear");
		expect(g.height).toBe(36);
		expect(p.height).toBe(g.height);
		expect(Math.abs(p.y - g.y)).toBeLessThanOrEqual(1);
		expect(g.x).toBeGreaterThan(p.x + p.width);
		expect((await headerLayout(page)).besideTitle).toBe(true);
	}
});

test("on a phone an open job keeps its actions and moves the board's behind …", async ({
	page,
}) => {
	await page.setViewportSize({ width: 390, height: 800 });
	await page.goto(`${A}/projects/o%2Fr/jobs/1013`);
	await expect(page.getByTestId("job-detail-actions")).toBeVisible();
	await expect(page.getByTestId("project-settings-link")).toHaveCount(0);
	await page.getByTestId("header-overflow").click();
	const items = page.getByTestId("header-overflow-items");
	await expect(items.getByTestId("logs-toggle")).toBeVisible();
	await expect(items.getByTestId("project-settings-link")).toBeVisible();
	// Wider, the board's actions are back in the row.
	await page.keyboard.press("Escape");
	await page.setViewportSize({ width: 768, height: 800 });
	await expect(page.getByTestId("header-overflow")).toHaveCount(0);
	await expect(
		page.getByTestId("settings-actions").getByTestId("project-settings-link"),
	).toBeVisible();
});
