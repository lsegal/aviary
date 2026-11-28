import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// On a phone the document itself never scrolls; only the panes inside the
// app shell do (#1656). Anything sized by 100vh is taller than the screen
// while the address bar shows, which used to let the whole shell scroll,
// pushing the header off the top and leaving an empty band at the bottom.

const NOW = new Date().toISOString();

/** Mocks agent "Aviary" with a chat, channel "signal", a task and a project. */
async function mockApp(page: Page) {
	const config = {
		agents: [
			{
				name: "Aviary",
				model: "x/y",
				channels: [
					{ type: "signal", id: "signal", deliver_to: "+15550001111" },
				],
				tasks: [{ name: "release-aviary", every: "1h", prompt: "release" }],
				projects: [{ target: "o/r" }],
			},
		],
	};
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [{ id: "Aviary", name: "Aviary", model: "x/y", state: "idle" }],
		config_get: () => config,
		config_validate: [],
		auth_list: [],
		tool_list: [],
		job_list: [],
		job_query: [],
		task_list: [],
		session_list: [
			{ id: "main", agent_id: "Aviary", name: "main", created_at: NOW },
		],
		session_messages: Array.from({ length: 40 }, (_, i) => ({
			id: `m${i}`,
			session_id: "main",
			role: i % 2 ? "assistant" : "user",
			content: `message ${i} `.repeat(20),
			timestamp: NOW,
		})),
		channel_sessions: [],
	});
}

const ROUTES = [
	"/agents/Aviary/chat/main",
	"/agents/Aviary/projects",
	"/agents/Aviary/projects/o%2Fr",
	"/agents/Aviary/channels/signal",
	"/agents/Aviary/tasks",
	"/system/settings",
	"/system/overview",
];

for (const viewport of [
	{ width: 375, height: 667 },
	{ width: 412, height: 600 },
]) {
	test(`the document never scrolls on a ${viewport.width}x${viewport.height} phone`, async ({
		browser,
	}) => {
		const context = await browser.newContext({
			viewport,
			hasTouch: false,
			isMobile: false,
			ignoreHTTPSErrors: true,
		});
		const page = await context.newPage();
		await mockApp(page);

		// The shell clips the document and stops overscroll from chaining to
		// it, so pull-to-refresh can't reveal a gap below the app.
		await page.goto(ROUTES[0]);
		const shell = await page.evaluate(() =>
			[document.documentElement, document.body].map((el) => {
				const s = getComputedStyle(el);
				return [s.overflowX, s.overflowY, s.overscrollBehaviorY].join(" ");
			}),
		);
		expect(shell).toEqual(["hidden hidden none", "hidden hidden none"]);

		for (const url of ROUTES) {
			await page.goto(url);
			await expect(page.locator("#app header").first(), url).toBeVisible();
			await expect(page.getByTestId("page-main"), url).not.toBeEmpty();
			const doc = await page.evaluate(() => {
				const el = document.scrollingElement as HTMLElement;
				window.scrollTo(0, 500);
				el.scrollTop = 500;
				return {
					overflow: el.scrollHeight - window.innerHeight,
					scrollY: window.scrollY,
				};
			});
			expect(doc.overflow, url).toBeLessThanOrEqual(0);
			expect(doc.scrollY, url).toBe(0);
			// The mobile header stays pinned to the top of the screen.
			const header = await page.locator("#app header").first().boundingBox();
			expect(header?.y, url).toBe(0);
		}
		await context.close();
	});
}

test("a long chat scrolls inside its pane, not the document", async ({
	browser,
}) => {
	const context = await browser.newContext({
		viewport: { width: 375, height: 667 },
		hasTouch: false,
		isMobile: false,
		ignoreHTTPSErrors: true,
	});
	const page = await context.newPage();
	await mockApp(page);
	await page.goto("/agents/Aviary/chat/main");
	await expect(page.getByText("message 39").first()).toBeVisible();
	const scrollers = await page.evaluate(
		() =>
			[...document.querySelectorAll<HTMLElement>("#app *")].filter((el) => {
				const y = getComputedStyle(el).overflowY;
				return (
					(y === "auto" || y === "scroll") && el.scrollHeight > el.clientHeight
				);
			}).length,
	);
	expect(scrollers).toBeGreaterThan(0);
	const overflow = await page.evaluate(
		() => document.documentElement.scrollHeight - window.innerHeight,
	);
	expect(overflow).toBeLessThanOrEqual(0);
	await context.close();
});
