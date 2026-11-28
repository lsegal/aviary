import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { goto, renderApp } from "../component-test/app";
import { mockMCP, routeWebSocket } from "../component-test/server";
// The pages this file renders (see component-test/routes.ts).
import "../views/ChatView.vue";
import "../views/OverviewView.vue";
import "../views/SettingsView.vue";

/*
 * A chat's running turn lives on the server, so a client that opens or
 * returns to the chat mid-turn shows the same "…" indicator, tool calls and
 * partial text as the one that sent the message (#764). The two-client
 * scenario stays in web/e2e/chat-live-turn.spec.ts: it needs two apps live
 * at once.
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

/** The mock server's state. */
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

	attach() {
		routeWebSocket(/\/api\/ws/, (ws) => {
			this.sockets.push(ws);
		});
		mockMCP({
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

const toolCalls = () => page.getByTestId("chat-tool-call");
const count = (loc: { elements(): Element[] }) => loc.elements().length;

async function expectLiveTurn() {
	await expect.poll(() => count(toolCalls())).toBe(2);
	await expect.element(toolCalls().nth(0)).toMatchTextContent("exec");
	await expect.element(toolCalls().nth(0)).not.toHaveAttribute("data-pending");
	await expect.element(toolCalls().nth(1)).toMatchTextContent("web_search");
	await expect
		.element(toolCalls().nth(1))
		.toHaveAttribute("data-pending", "true");
	await expect.element(page.getByText("Looking at the logs")).toBeVisible();
	// The running call's chip is the turn's status; no typing bubble too.
	await expect.poll(() => count(page.getByTestId("chat-typing"))).toBe(0);
}

test("navigating away mid-turn and back restores the indicator and tool calls", async () => {
	const server = new Server();
	server.attach();
	await renderApp("/agents/assistant/chat");
	await expectLiveTurn();

	await goto("/system/overview");
	await expect.poll(() => count(page.getByTestId("chat-typing"))).toBe(0);
	await goto("/agents/assistant/chat");
	await expectLiveTurn();
});

test("a tab made visible again mid-turn shows the current turn", async () => {
	const server = new Server();
	const running = server.turn;
	server.turn = { ...running, processing: false, items: [] };
	server.attach();
	await renderApp("/agents/assistant/chat");
	await expect.element(page.getByText("Check the build")).toBeVisible();
	await expect.poll(() => count(page.getByTestId("chat-typing"))).toBe(0);

	// The turn started while the tab was hidden and its events were missed.
	server.turn = running;
	document.dispatchEvent(new Event("visibilitychange"));
	await expectLiveTurn();
});

test("restoring a turn already partly stored shows each tool call once", async () => {
	const server = new Server();
	server.messages.push(toolRecord("call_1", "exec", new Date().toISOString()));
	server.attach();
	await renderApp("/agents/assistant/chat");
	await expectLiveTurn();
});
