import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
	await page.addInitScript(() => {
		const sockets: MockWebSocket[] = [];
		class MockWebSocket {
			static CONNECTING = 0;
			static OPEN = 1;
			static CLOSING = 2;
			static CLOSED = 3;

			readyState = MockWebSocket.OPEN;
			onopen: ((this: WebSocket, ev: Event) => unknown) | null = null;
			onclose: ((this: WebSocket, ev: CloseEvent) => unknown) | null = null;
			onerror: ((this: WebSocket, ev: Event) => unknown) | null = null;
			onmessage:
				| ((this: WebSocket, ev: MessageEvent<string>) => unknown)
				| null = null;

			constructor() {
				sockets.push(this);
			}

			addEventListener() {}

			removeEventListener() {}

			send() {}

			close() {
				this.readyState = MockWebSocket.CLOSED;
			}
		}

		Object.defineProperty(window, "WebSocket", {
			configurable: true,
			writable: true,
			value: MockWebSocket,
		});
		(
			window as typeof window & { __mockWebSockets?: MockWebSocket[] }
		).__mockWebSockets = sockets;
	});
	await mockMCP(page, {
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "anthropic/claude-sonnet-4-5",
				fallbacks: [],
				state: "idle",
			},
		],
		session_list: [
			{
				id: "s1",
				agent_id: "a1",
				name: "main",
				created_at: "2026-03-12T12:00:00Z",
			},
		],
		session_messages: [
			{
				id: "m1",
				session_id: "s1",
				role: "user",
				content: "first prompt",
				timestamp: "2026-03-12T12:00:00Z",
			},
			{
				id: "m2",
				session_id: "s1",
				role: "assistant",
				content: "first response",
				timestamp: "2026-03-12T12:00:01Z",
			},
			{
				id: "m3",
				session_id: "s1",
				role: "user",
				content: "second prompt",
				timestamp: "2026-03-12T12:00:02Z",
			},
		],
		config_validate: [],
	});
});

test("capture747", async ({
	page,
}) => {
	const runs: Record<string, unknown>[] = [];
	let failing = true;
	await mockMCP(page, {
		agent_list: [
			{
				id: "a1",
				name: "assistant",
				model: "claude/opus",
				fallbacks: [],
				state: "idle",
			},
		],
		session_list: [
			{
				id: "s1",
				agent_id: "a1",
				name: "main",
				created_at: "2026-03-12T12:00:00Z",
			},
		],
		config_validate: [],
		session_messages: () =>
			runs.length > 1
				? [
						{
							id: "m1",
							session_id: "s1",
							role: "user",
							content: "summarize",
							timestamp: "2026-03-12T12:00:00Z",
						},
						{
							id: "m2",
							session_id: "s1",
							role: "assistant",
							content: "all good",
							timestamp: "2026-03-12T12:00:01Z",
						},
					]
				: [],
		agent_run: (args?: Record<string, unknown>) => {
			runs.push(args ?? {});
			if (failing) throw new Error("MCP error: 502 Bad Gateway");
			return { stream: ["all good"], result: "" };
		},
	});
	await page.goto("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await expect(input).toBeEnabled();
	await input.fill("summarize");
	await input.press("Enter");

	// The error hangs off the sent message's bubble, not as its own entry.
	const sent = page.locator(".text-right").filter({ hasText: "summarize" });
	const error = sent.getByTestId("chat-message-error");
	await expect(error).toContainText("Error: MCP error: 502 Bad Gateway");
	await expect(error).toContainText("claude/opus");
	await expect(page.getByTestId("chat-message-error")).toHaveCount(1);
	const bubble = await sent.locator(".bg-blue-600").boundingBox();
	const attached = await error.boundingBox();
	expect(bubble && attached).toBeTruthy();
	if (bubble && attached) {
		// Staggered: tucked under the bubble and inset from its right edge.
		expect(attached.y).toBeLessThan(bubble.y + bubble.height + 1);
		expect(attached.x + attached.width).toBeLessThan(bubble.x + bubble.width);
	}

	await page.locator("main, body").first().screenshot({ path: "/tmp/shot747-error.png" });
	// Retrying while the provider still fails updates the error in place.
	await sent.getByTestId("chat-message-retry").click();
	await expect.poll(() => runs.length).toBe(2);
	expect(runs[1]).toMatchObject({ message: "summarize", retry: true });
	await expect(error).toContainText("502 Bad Gateway");
	await expect(page.getByTestId("chat-message-error")).toHaveCount(1);

	// Once it recovers, Retry lands the reply and the error is gone.
	failing = false;
	await sent.getByTestId("chat-message-retry").click();
	await expect.poll(() => runs.length).toBe(3);
	expect(runs[2]).toMatchObject({ message: "summarize", retry: true });
	await expect(page.getByText("all good")).toBeVisible();
	await expect(page.getByTestId("chat-message-error")).toHaveCount(0);
	await expect(page.getByText("summarize", { exact: true })).toHaveCount(1);
	await page.locator("main, body").first().screenshot({ path: "/tmp/shot747-recovered.png" });
});
