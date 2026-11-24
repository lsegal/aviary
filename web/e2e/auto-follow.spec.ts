import { expect, type Locator, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

/*
 * Every chat and live log view shares one auto-follow behavior (#1072): it
 * opens at its newest line, follows new content while the user is at the
 * bottom, and stops following with a "More below" pill once they scroll up.
 */

const NOW = new Date().toISOString();

interface TurnItem {
	text?: string;
	tool?: { id: string; name: string; done?: boolean };
}

/** The mock server: a long stored history plus a running turn. */
class Server {
	messages: object[] = Array.from({ length: 40 }, (_, i) => ({
		id: `m${i}`,
		role: i % 2 ? "assistant" : "user",
		content: `Message ${i}\n\nwith a second paragraph to give it height`,
		timestamp: NOW,
	}));
	items: TurnItem[] = [{ text: "Working" }];
	sockets: Array<{ send: (m: string) => void }> = [];
	session = "";

	/** Pushes the running turn to every client. */
	broadcast() {
		const turn = {
			agent_id: "assistant",
			session_id: this.session,
			processing: true,
			items: this.items,
		};
		const frame = JSON.stringify({
			type: "session_turn",
			agent_id: "assistant",
			session_id: this.session,
			turn,
		});
		for (const ws of this.sockets) ws.send(frame);
	}

	/** Streams more text into the turn's reply. */
	stream(text: string) {
		const last = this.items.at(-1);
		if (last?.text !== undefined) last.text += text;
		else this.items.push({ text });
		this.broadcast();
	}

	/** Starts a new tool call: one more entry in the chat. */
	addTool(id: string) {
		this.items.push({ tool: { id, name: "exec" } });
		this.broadcast();
	}

	async attach(page: Page, opts: { workflow?: boolean } = {}) {
		await setAuthToken(page);
		await page.routeWebSocket(/\/api\/ws/, (ws) => {
			this.sockets.push(ws);
		});
		await mockMCP(page, {
			agent_list: [
				{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
			],
			config_get: () => ({
				agents: [
					{
						name: "assistant",
						model: "x/y",
						channels: [{ type: "slack", id: "team", name: "Team" }],
						tasks: [
							opts.workflow
								? {
										name: "report",
										trigger: "message",
										channel: "slack:team",
										prompt: "Report",
									}
								: { name: "report", every: "15m", prompt: "Report" },
						],
					},
				],
			}),
			config_validate: [],
			job_list: [],
			auth_list: [],
			tool_list: [],
			task_list: () => [
				{
					id: "assistant/report",
					agent_name: "assistant",
					name: "report",
					enabled: true,
					history: [],
				},
			],
			task_session: () => ({
				id: "task-report",
				agent_id: "assistant",
				name: "task:report",
			}),
			channel_sessions: () => [
				{
					id: "channel:slack:team",
					agent_id: "assistant",
					name: "channel:slack:team",
					created_at: NOW,
					own: true,
				},
			],
			session_list: [
				{
					id: "main",
					agent_id: "assistant",
					name: "main",
					created_at: NOW,
					is_processing: true,
				},
			],
			session_messages: (args?: Record<string, unknown>) => {
				this.session = String(args?.session_id ?? "");
				return this.messages;
			},
			session_turn: (args?: Record<string, unknown>) => ({
				agent_id: "assistant",
				session_id: String(args?.session_id ?? ""),
				processing: true,
				items: this.items,
			}),
			agent_run: { stream: ["on it"], result: "" },
		});
	}
}

const distanceFromBottom = (scroller: Locator) =>
	scroller.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight);

async function expectAtBottom(scroller: Locator) {
	await expect.poll(() => distanceFromBottom(scroller)).toBeLessThanOrEqual(8);
}

/** Scrolls the view to its top, as a user scrolling up would. */
async function scrollToTop(scroller: Locator) {
	await scroller.evaluate((el) => {
		el.scrollTop = 0;
	});
}

const HOSTS = [
	{
		name: "agent chat",
		path: "/agents/assistant/chat",
		scope: (page: Page) => page.locator("body"),
	},
	{
		name: "channel chat",
		path: "/agents/assistant/channels/Team",
		scope: (page: Page) => page.getByTestId("channel-chat"),
	},
	{
		name: "task session",
		path: "/agents/assistant/tasks/report",
		scope: (page: Page) => page.getByTestId("task-chat"),
	},
	{
		name: "workflow session",
		path: "/agents/assistant/tasks/report",
		workflow: true,
		scope: (page: Page) => page.getByTestId("task-chat"),
	},
];

for (const host of HOSTS) {
	test(`${host.name} opens at the bottom, follows, and pauses with the pill`, async ({
		page,
	}) => {
		const server = new Server();
		await server.attach(page, { workflow: host.workflow });
		await page.goto(host.path);
		const chat = host.scope(page);
		const scroller = chat.getByTestId("chat-messages");
		const pill = chat.getByTestId("chat-more-below");
		await expect(chat.getByText("Message 39")).toBeVisible();
		await expect
			.poll(() => scroller.evaluate((el) => el.scrollHeight > el.clientHeight))
			.toBe(true);
		await expectAtBottom(scroller);
		await expect.poll(() => server.sockets.length).toBeGreaterThan(0);

		// Streaming text keeps a following view pinned.
		for (let i = 0; i < 5; i++) server.stream("\nmore streamed output line");
		await expect(chat.getByText(/more streamed output line/)).toBeVisible();
		await expectAtBottom(scroller);
		await expect(pill).toBeHidden();

		// Scrolled up, new content doesn't move the view, and the pill counts it.
		await scrollToTop(scroller);
		await expect(pill).toBeVisible();
		await expect(pill).toHaveText("More below ↓");
		server.addTool("call_1");
		server.stream("\nafter the tool");
		await expect(chat.getByTestId("chat-tool-call")).toHaveCount(1);
		await expect(pill).toHaveText("2 new · More below ↓");
		expect(await scroller.evaluate((el) => el.scrollTop)).toBe(0);

		// The pill returns to the end and follows again.
		await pill.click();
		await expectAtBottom(scroller);
		await expect(pill).toBeHidden();
		server.stream("\nfollowing again");
		await expect(chat.getByText(/following again/)).toBeVisible();
		await expectAtBottom(scroller);

		// Scrolling back down by hand follows again too.
		await scrollToTop(scroller);
		await expect(pill).toBeVisible();
		await scroller.evaluate((el) => {
			el.scrollTop = el.scrollHeight;
		});
		await expect(pill).toBeHidden();
		server.stream("\nstill following");
		await expect(chat.getByText(/still following/)).toBeVisible();
		await expectAtBottom(scroller);
	});
}

test("sending a message scrolls to the bottom and follows again", async ({
	page,
}) => {
	const server = new Server();
	server.items = [];
	await server.attach(page);
	await page.goto("/agents/assistant/chat");
	const scroller = page.getByTestId("chat-messages");
	await expect(page.getByText("Message 39")).toBeVisible();
	await scrollToTop(scroller);
	await expect(page.getByTestId("chat-more-below")).toBeVisible();

	await page.getByRole("textbox").first().fill("hello there");
	await page.keyboard.press("Enter");
	await expect(page.getByTestId("chat-user-message").last()).toContainText(
		"hello there",
	);
	await expectAtBottom(scroller);
	await expect(page.getByTestId("chat-more-below")).toBeHidden();
});

test("a chat opens at the bottom after a late image loads", async ({
	page,
}) => {
	const server = new Server();
	server.items = [];
	server.messages.push({
		id: "img",
		role: "assistant",
		content: "",
		media_urls: ["/late-image.png"],
		timestamp: NOW,
	});
	let release = () => {};
	const released = new Promise<void>((resolve) => {
		release = resolve;
	});
	await page.route("**/late-image.png", async (route) => {
		await released;
		await route.fulfill({
			contentType: "image/svg+xml",
			body: '<svg xmlns="http://www.w3.org/2000/svg" width="200" height="300"><rect width="200" height="300" fill="teal"/></svg>',
		});
	});
	await server.attach(page);
	await page.goto("/agents/assistant/chat");
	const scroller = page.getByTestId("chat-messages");
	await expect(page.getByText("Message 39")).toBeVisible();
	await expectAtBottom(scroller);
	const before = await scroller.evaluate((el) => el.scrollHeight);

	release();
	await expect
		.poll(() => scroller.evaluate((el) => el.scrollHeight))
		.toBeGreaterThan(before + 100);
	await expectAtBottom(scroller);
});

test("a deep link to a message opens there instead of the bottom", async ({
	page,
}) => {
	const server = new Server();
	server.items = [];
	await server.attach(page);
	await page.goto("/agents/assistant/chat#message-m5");
	const scroller = page.getByTestId("chat-messages");
	const target = page.locator("#message-m5");
	await expect(target).toBeInViewport();
	expect(await distanceFromBottom(scroller)).toBeGreaterThan(100);
	await expect(page.getByTestId("chat-more-below")).toBeVisible();
});

test("the pill scrolls instantly under prefers-reduced-motion", async ({
	page,
}) => {
	const server = new Server();
	server.items = [];
	await server.attach(page);
	await page.goto("/agents/assistant/chat");
	const scroller = page.getByTestId("chat-messages");
	await expect(page.getByText("Message 39")).toBeVisible();

	// The click and the measurement run in one task, before any smooth
	// scroll could advance.
	const clickAndMeasure = () =>
		scroller.evaluate((el) => {
			el.scrollTop = 0;
			el.dispatchEvent(new Event("scroll"));
			return new Promise<number>((resolve) =>
				requestAnimationFrame(() => {
					(
						document.querySelector(
							"[data-testid=chat-more-below]",
						) as HTMLElement
					).click();
					resolve(el.scrollHeight - el.scrollTop - el.clientHeight);
				}),
			);
		});

	await page.emulateMedia({ reducedMotion: "no-preference" });
	expect(await clickAndMeasure()).toBeGreaterThan(8);
	await expectAtBottom(scroller);

	await page.emulateMedia({ reducedMotion: "reduce" });
	expect(await clickAndMeasure()).toBeLessThanOrEqual(8);
});

test("a following chat stays at the bottom when the on-screen keyboard opens and closes", async ({
	page,
}) => {
	await page.setViewportSize({ width: 390, height: 844 });
	const server = new Server();
	server.items = [];
	await server.attach(page);
	await page.goto("/agents/assistant/chat");
	const scroller = page.getByTestId("chat-messages");
	await expect(page.getByText("Message 39")).toBeVisible();
	await expectAtBottom(scroller);

	// The keyboard takes the bottom of the viewport, then gives it back.
	await page.setViewportSize({ width: 390, height: 500 });
	await expectAtBottom(scroller);
	await page.setViewportSize({ width: 390, height: 844 });
	await expectAtBottom(scroller);
});

test("the system log pane follows, pauses with the pill, and keeps its place loading older lines", async ({
	page,
}) => {
	const entry = (seq: number) => ({
		seq,
		ts: NOW,
		level: "info",
		component: "server",
		msg: `log line ${seq}`,
	});
	await page.route(/\/api\/logs\/history\?/, (route) =>
		route.fulfill({
			contentType: "application/json",
			body: JSON.stringify({
				entries: Array.from({ length: 50 }, (_, i) => entry(100 + i)),
				hasMore: true,
			}),
		}),
	);
	// One stream of live lines; a reconnect waits forever.
	let streams = 0;
	await page.route(/\/api\/logs\?/, (route) => {
		if (streams++) return;
		return route.fulfill({
			contentType: "text/event-stream",
			body: Array.from(
				{ length: 60 },
				(_, i) => `data: ${JSON.stringify(entry(200 + i))}\n\n`,
			).join(""),
		});
	});
	await setAuthToken(page);
	await mockMCP(page, { config_validate: [], agent_list: [] });
	await page.goto("/system/logs");
	const scroller = page.getByTestId("logs-scroll");
	await expect(scroller.getByText("log line 259")).toBeVisible();
	await expectAtBottom(scroller);

	await scrollToTop(scroller);
	await expect(page.getByTestId("logs-more-below")).toBeVisible();

	// Older lines load above without moving what's on screen.
	const first = scroller.getByText("log line 200", { exact: true });
	await expect(first).toBeInViewport();
	await scroller.getByRole("button", { name: /Load previous/ }).click();
	await expect(
		scroller.getByText("log line 149", { exact: true }),
	).toBeAttached();
	await expect
		.poll(() => scroller.evaluate((el) => el.scrollTop))
		.toBeGreaterThan(100);
	await expect(first).toBeInViewport();

	await page.getByTestId("logs-more-below").click();
	await expectAtBottom(scroller);
	await expect(page.getByTestId("logs-more-below")).toBeHidden();
});
