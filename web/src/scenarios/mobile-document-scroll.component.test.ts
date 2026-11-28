import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { goto, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";

// On a phone the document itself never scrolls; only the panes inside the
// app shell do (#1656). Anything sized by 100vh is taller than the screen
// while the address bar shows, which used to let the whole shell scroll,
// pushing the header off the top and leaving an empty band at the bottom.
// The test iframe is the "screen": page.viewport sizes it, and its document
// carries the app's style.css, so its html/body play the document's part.

const NOW = new Date().toISOString();

/** Mocks agent "Aviary" with a chat, channel "signal", a task and a project. */
function mockApp() {
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
	mockMCP({
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
	test(`the document never scrolls on a ${viewport.width}x${viewport.height} phone`, async () => {
		await page.viewport(viewport.width, viewport.height);
		mockApp();

		// The shell clips the document and stops overscroll from chaining to
		// it, so pull-to-refresh can't reveal a gap below the app.
		await renderApp(ROUTES[0]);
		const shell = [document.documentElement, document.body].map((el) => {
			const s = getComputedStyle(el);
			return [s.overflowX, s.overflowY, s.overscrollBehaviorY].join(" ");
		});
		expect(shell).toEqual(["hidden hidden none", "hidden hidden none"]);

		const header = page.locator("#app header").first();
		for (const url of ROUTES) {
			await goto(url);
			await expect.element(header, { message: url }).toBeVisible();
			await expect
				.poll(() => page.getByTestId("page-main").element().childElementCount, {
					message: url,
				})
				.toBeGreaterThan(0);
			const el = document.scrollingElement as HTMLElement;
			window.scrollTo(0, 500);
			el.scrollTop = 500;
			expect(el.scrollHeight - window.innerHeight, url).toBeLessThanOrEqual(0);
			expect(window.scrollY, url).toBe(0);
			// The mobile header stays pinned to the top of the screen.
			expect(header.element().getBoundingClientRect().y, url).toBe(0);
		}
	});
}

test("a long chat scrolls inside its pane, not the document", async () => {
	await page.viewport(375, 667);
	mockApp();
	await renderApp("/agents/Aviary/chat/main");
	await expect.element(page.getByText("message 39").first()).toBeVisible();
	const scrollers = [
		...document.querySelectorAll<HTMLElement>("#app *"),
	].filter((el) => {
		const y = getComputedStyle(el).overflowY;
		return (
			(y === "auto" || y === "scroll") && el.scrollHeight > el.clientHeight
		);
	}).length;
	expect(scrollers).toBeGreaterThan(0);
	expect(
		document.documentElement.scrollHeight - window.innerHeight,
	).toBeLessThanOrEqual(0);
});
