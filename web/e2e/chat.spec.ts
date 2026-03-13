import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
	await page.addInitScript(() => {
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

test("up arrow recalls previous chat messages", async ({ page }) => {
	await page.goto("/chat");

	const input = page.getByPlaceholder("Type a message or paste an image…");
	await expect(input).toBeEnabled();

	await input.fill("draft message");
	await input.press("ArrowUp");
	await expect(input).toHaveValue("second prompt");

	await input.press("ArrowUp");
	await expect(input).toHaveValue("first prompt");

	await input.press("ArrowDown");
	await expect(input).toHaveValue("second prompt");

	await input.press("ArrowDown");
	await expect(input).toHaveValue("draft message");
});
