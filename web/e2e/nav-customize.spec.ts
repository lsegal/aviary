import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// Customizing the navigation (#1003) by touch. The rest of the sidebar
// editor's scenarios are component tests (web/src/scenarios/
// nav-customize*.component.test.ts, #1636); this one stays here because it
// needs real touch input (CDP touch events, native scrolling and fling),
// which component tests can't produce.

const DEFAULT_PAGES = [
	"chat",
	"projects",
	"channels",
	"tasks",
	"files",
	"settings",
];

async function mockAgents(page: Page, names: string[]) {
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [],
		job_list: [],
		config_validate: [],
		session_list: [],
		config_get: {
			agents: names.map((name) => ({
				name,
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
			})),
		},
	});
}

function editRow(page: Page, list: string, key: string) {
	return page.getByTestId(`nav-edit-${list}`).locator(`[data-key="${key}"]`);
}

test.describe("dragging on touch", () => {
	test.use({
		hasTouch: true,
		isMobile: true,
		viewport: { width: 390, height: 844 },
	});

	test("a long press picks the row up; a swipe does not", async ({ page }) => {
		await mockAgents(page, ["assistant"]);
		await page.goto("/agents/assistant/chat");
		await page.locator("header").getByTestId("gear-menu-button").click();
		await page.getByTestId("gear-menu-edit-nav").click();
		await expect(page.getByTestId("nav-edit-agentPages")).toBeVisible();
		const cdp = await page.context().newCDPSession(page);
		const touch = (
			type: "touchStart" | "touchMove" | "touchEnd",
			x?: number,
			y?: number,
		) =>
			cdp.send("Input.dispatchTouchEvent", {
				type,
				touchPoints: x === undefined ? [] : [{ x, y: y ?? 0 }],
			});
		const label = async (key: string) => {
			const b = await editRow(page, "agentPages", key)
				.locator("span")
				.first()
				.boundingBox();
			return { x: (b?.x ?? 0) + 10, y: (b?.y ?? 0) + (b?.height ?? 0) / 2 };
		};

		// A quick swipe over a row moves nothing.
		let from = await label("tasks");
		let to = await label("chat");
		await touch("touchStart", from.x, from.y);
		for (let i = 1; i <= 6; i++)
			await touch("touchMove", from.x, from.y + ((to.y - from.y) * i) / 6);
		await touch("touchEnd");
		expect(await rowKeys(page, "agentPages")).toEqual(DEFAULT_PAGES);

		// A long press anywhere on the row picks it up. The swipe flung the
		// page, so let the fling settle, then bring the rows clear of the
		// autoscrolling edges.
		const scroller = page.getByTestId("settings-scroll");
		let last = -1;
		await expect
			.poll(async () => {
				const top = await scroller.evaluate((el) => el.scrollTop);
				const settled = top === last;
				last = top;
				return settled;
			})
			.toBe(true);
		await editRow(page, "agentPages", "chat").evaluate((el) =>
			el.scrollIntoView({ block: "center" }),
		);
		from = await label("tasks");
		to = await label("chat");
		await touch("touchStart", from.x, from.y);
		await page.waitForTimeout(500);
		const placeholder = page
			.getByTestId("nav-edit-agentPages")
			.getByTestId("sortable-placeholder");
		await expect(placeholder).toBeVisible();
		for (let i = 1; i <= 8; i++)
			await touch("touchMove", from.x, from.y + ((to.y - from.y - 4) * i) / 8);
		await touch("touchEnd");
		await expect(placeholder).toHaveCount(0);
		await expect
			.poll(() => rowKeys(page, "agentPages"))
			.toEqual(["tasks", "chat", "projects", "channels", "files", "settings"]);
	});
});

/** A section's rows in the sidebar editor, by key, in order. */
function rowKeys(page: Page, list: string): Promise<string[]> {
	return page
		.getByTestId(`nav-edit-${list}`)
		.getByTestId("nav-edit-row")
		.evaluateAll((els) => els.map((el) => el.getAttribute("data-key") ?? ""));
}
