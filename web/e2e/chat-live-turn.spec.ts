import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { type Browser, expect, type Page, test } from "./helpers/test";

/*
 * A chat's running turn lives on the server, so a client that opens or
 * returns to the chat mid-turn shows the same "…" indicator, tool calls and
 * partial text as the one that sent the message (#764).
 */

interface TurnTool {
	id: string;
	name: string;
	args?: Record<string, unknown>;
	result?: string;
	done?: boolean;
}

interface Turn {
	agent_id: string;
	session_id: string;
	processing: boolean;
	items: Array<{ text?: string; tool?: TurnTool }>;
}

/** The mock server's state, shared by every page of a test. */
class Server {
	messages: object[] = [
		{
			id: "m1",
			session_id: "s1",
			role: "user",
			content: "Check the build",
			timestamp: new Date().toISOString(),
		},
	];
	turn: Turn = {
		agent_id: "assistant",
		session_id: "s1",
		processing: true,
		items: [
			{ tool: { id: "call_1", name: "exec", args: { cmd: "ls" }, done: true } },
			{ text: "Looking at the logs" },
			{ tool: { id: "call_2", name: "web_search", args: { q: "ci" } } },
		],
	};
	sockets: Array<{ send: (m: string) => void }> = [];

	/** Pushes the current turn to every connected client. */
	broadcast() {
		const frame = JSON.stringify({
			type: "session_turn",
			agent_id: this.turn.agent_id,
			session_id: this.turn.session_id,
			turn: this.turn,
		});
		for (const ws of this.sockets) ws.send(frame);
	}

	/** Ends the turn as the server does: the reply and finished calls are
	 * stored, then every client hears the turn is over. */
	finish() {
		const ts = new Date().toISOString();
		this.messages.push(
			toolRecord("call_1", "exec", ts),
			toolRecord("call_2", "web_search", ts),
			toolRecord("call_3", "file_read", ts),
			{
				id: "m9",
				session_id: "s1",
				role: "assistant",
				content: "The build is green.",
				timestamp: ts,
			},
		);
		this.turn = { ...this.turn, processing: false, items: [] };
		this.broadcast();
		for (const ws of this.sockets) {
			ws.send(
				JSON.stringify({
					type: "session_processing",
					agent_id: "assistant",
					session_id: "s1",
					is_processing: false,
				}),
			);
		}
	}

	async attach(page: Page) {
		await setAuthToken(page);
		await page.routeWebSocket(/\/api\/ws/, (ws) => {
			this.sockets.push(ws);
		});
		await mockMCP(page, {
			agent_list: [
				{
					id: "assistant",
					name: "assistant",
					model: "anthropic/claude-sonnet-4-5",
					state: "idle",
				},
			],
			session_list: () => [
				{
					id: "s1",
					agent_id: "assistant",
					name: "main",
					created_at: new Date().toISOString(),
					is_processing: this.turn.processing,
				},
			],
			session_messages: () => this.messages,
			session_turn: () => this.turn,
			config_validate: [],
		});
	}
}

function toolRecord(id: string, name: string, timestamp: string) {
	return {
		id: `rec_${id}`,
		session_id: "s1",
		role: "tool",
		content: JSON.stringify({ id, name, args: {}, result: "ok" }),
		timestamp,
	};
}

const toolCalls = (page: Page) => page.getByTestId("chat-tool-call");

async function expectLiveTurn(page: Page) {
	await expect(toolCalls(page)).toHaveCount(2);
	await expect(toolCalls(page).nth(0)).toContainText("exec");
	await expect(toolCalls(page).nth(0)).not.toHaveAttribute("data-pending");
	await expect(toolCalls(page).nth(1)).toContainText("web_search");
	await expect(toolCalls(page).nth(1)).toHaveAttribute("data-pending", "true");
	await expect(page.getByText("Looking at the logs")).toBeVisible();
	// The running call's chip is the turn's status; no typing bubble too.
	await expect(page.getByTestId("chat-typing")).toHaveCount(0);
}

async function newPage(browser: Browser, server: Server) {
	const context = await browser.newContext({ ignoreHTTPSErrors: true });
	const page = await context.newPage();
	await server.attach(page);
	return page;
}

test("navigating away mid-turn and back restores the indicator and tool calls", async ({
	page,
}) => {
	const server = new Server();
	await server.attach(page);
	await page.goto("/agents/assistant/chat");
	await expectLiveTurn(page);

	await page.goto("/system/overview");
	await expect(page.getByTestId("chat-typing")).toHaveCount(0);
	await page.goto("/agents/assistant/chat");
	await expectLiveTurn(page);
});

test("a second client mid-turn shows the same state and live updates, then the reply once", async ({
	browser,
}) => {
	const server = new Server();
	const first = await newPage(browser, server);
	const second = await newPage(browser, server);
	await first.goto("/agents/assistant/chat");
	await second.goto("/agents/assistant/chat");
	await expectLiveTurn(first);
	await expectLiveTurn(second);
	await expect.poll(() => server.sockets.length).toBeGreaterThanOrEqual(2);

	// The running call finishes and another starts.
	server.turn.items[2].tool = {
		...(server.turn.items[2].tool as TurnTool),
		result: "found",
		done: true,
	};
	server.broadcast();
	// With no call running, the turn is thinking again.
	for (const page of [first, second]) {
		await expect(page.getByTestId("chat-typing")).toBeVisible();
	}
	server.turn.items.push({ tool: { id: "call_3", name: "file_read" } });
	server.broadcast();
	for (const page of [first, second]) {
		await expect(toolCalls(page)).toHaveCount(3);
		await expect(toolCalls(page).nth(1)).not.toHaveAttribute("data-pending");
		await expect(toolCalls(page).nth(2)).toContainText("file_read");
		await expect(toolCalls(page).nth(2)).toHaveAttribute(
			"data-pending",
			"true",
		);
	}

	server.finish();
	for (const page of [first, second]) {
		await expect(page.getByTestId("chat-typing")).toHaveCount(0);
		await expect(page.getByText("The build is green.")).toHaveCount(1);
		await expect(page.getByText("Looking at the logs")).toHaveCount(0);
		await expect(toolCalls(page)).toHaveCount(3);
		await expect(page.locator("[data-pending]")).toHaveCount(0);
	}
});

test("a tab made visible again mid-turn shows the current turn", async ({
	page,
}) => {
	const server = new Server();
	const running = server.turn;
	server.turn = { ...running, processing: false, items: [] };
	await server.attach(page);
	await page.goto("/agents/assistant/chat");
	await expect(page.getByText("Check the build")).toBeVisible();
	await expect(page.getByTestId("chat-typing")).toHaveCount(0);

	// The turn started while the tab was hidden and its events were missed.
	server.turn = running;
	await page.evaluate(() =>
		document.dispatchEvent(new Event("visibilitychange")),
	);
	await expectLiveTurn(page);
});

test("restoring a turn already partly stored shows each tool call once", async ({
	page,
}) => {
	const server = new Server();
	server.messages.push(toolRecord("call_1", "exec", new Date().toISOString()));
	await server.attach(page);
	await page.goto("/agents/assistant/chat");
	await expectLiveTurn(page);
});
