import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// Settings pages fit a phone: nothing widens the page or the settings
// scroller past the screen, so the header never slides off the left edge
// (#997). A fieldset's default min-inline-size used to let one long preview
// line widen the whole workflow form.

const NOW = new Date().toISOString();
const LONG = "the deploy failed again after the rollback finished ".repeat(6);

/** Mocks agent "Aviary" with channel "signal", a task, a project and long workflow previews. */
async function mockSettings(page: Page) {
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
		session_messages: [],
		channel_sessions: [
			{
				id: "channel:signal:signal",
				agent_id: "Aviary",
				name: "channel:signal:signal",
				created_at: NOW,
				own: true,
			},
		],
		workflow_entries: [{ entry: "+15550001111", title: "Alice" }],
		workflow_preview: [
			{
				entry: "+15550001111",
				sender: "Alice",
				text: LONG,
				timestamp: NOW,
				matches: true,
			},
			{
				entry: "+15550001111",
				sender: "Bob",
				text: "https://example.com/builds/1234567890/artifacts/logs/stderr-without-any-spaces.txt",
				timestamp: NOW,
				matches: true,
			},
		],
	});
}

/** Asserts neither the document nor the settings scroller scrolls sideways. */
async function expectNoHorizontalOverflow(page: Page, label: string) {
	await expect(page.getByTestId("page-header")).toBeVisible();
	const sizes = await page.evaluate(() => {
		const scroller = document.querySelector("[data-testid=settings-scroll]");
		return {
			page: document.documentElement.scrollWidth - window.innerWidth,
			scroller: scroller ? scroller.scrollWidth - scroller.clientWidth : 0,
		};
	});
	expect(sizes, label).toEqual({ page: 0, scroller: 0 });
	await expect(page.getByTestId("page-back")).toBeInViewport({ ratio: 1 });
	await expect(page.getByTestId("page-title")).toBeInViewport();
}

for (const width of [320, 375, 430]) {
	test(`settings pages fit a ${width}px phone`, async ({ browser }) => {
		const context = await browser.newContext({
			viewport: { width, height: 800 },
			hasTouch: false,
			isMobile: false,
			ignoreHTTPSErrors: true,
		});
		const page = await context.newPage();
		await mockSettings(page);

		for (const url of [
			"/agents/Aviary/channels/signal/settings",
			"/agents/Aviary/tasks?task=release-aviary",
			"/agents/Aviary/settings",
			"/projects/settings?agent=Aviary&project=o%2Fr",
			"/projects/settings",
			"/system/settings",
		]) {
			await page.goto(url);
			await expectNoHorizontalOverflow(page, url);
		}

		// The workflow form with its trigger section, a long preview and the
		// takeover warning showing.
		await page.goto("/agents/Aviary/channels/signal/settings?tab=workflows");
		await page.getByTestId("workflow-add").click();
		await page.getByTestId("workflow-match").fill("*");
		await expect(page.getByTestId("workflow-takeover-hint")).toBeVisible();
		await expect(page.getByTestId("workflow-preview-message")).toHaveCount(2);
		await expectNoHorizontalOverflow(page, "workflow form");
		const scroller = await page.getByTestId("settings-scroll").boundingBox();
		const hint = await page.getByTestId("workflow-takeover-hint").boundingBox();
		expect((hint?.x ?? 0) + (hint?.width ?? 0)).toBeLessThanOrEqual(
			(scroller?.x ?? 0) + (scroller?.width ?? 0),
		);
		await context.close();
	});
}
