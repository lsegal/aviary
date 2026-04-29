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

test("streaming response updates without dropping existing messages", async ({
	page,
}) => {
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
		agent_run: {
			stream: ["partial ", "response"],
			result: "",
		},
		config_validate: [],
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
			{
				id: "m4",
				session_id: "s1",
				role: "user",
				content: "stream this",
				timestamp: "2026-03-12T12:00:03Z",
			},
			{
				id: "m5",
				session_id: "s1",
				role: "assistant",
				content: "partial response",
				timestamp: "2026-03-12T12:00:04Z",
			},
		],
	});
	await page.goto("/chat");

	const input = page.getByPlaceholder("Type a message or paste an image…");
	await input.fill("stream this");
	await input.press("Enter");

	await expect(page.getByText("first prompt")).toBeVisible();
	await expect(page.getByText("first response")).toBeVisible();
	await expect(page.getByText("second prompt")).toBeVisible();
	await expect(page.getByText("partial response")).toBeVisible();
});

test("stale session reloads cannot overwrite newer messages", async ({
	page,
}) => {
	const oldMessages = [
		{
			id: "m1",
			session_id: "s1",
			role: "user",
			content: "first prompt",
			timestamp: "2026-03-12T12:00:00Z",
		},
	];
	const newMessages = [
		...oldMessages,
		{
			id: "m2",
			session_id: "s1",
			role: "assistant",
			content: "new streamed message",
			timestamp: "2026-03-12T12:00:01Z",
		},
	];
	let calls = 0;
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
		config_validate: [],
		session_messages: async () => {
			calls += 1;
			if (calls === 3) {
				await new Promise((resolve) => setTimeout(resolve, 150));
				return oldMessages;
			}
			return calls >= 4 ? newMessages : oldMessages;
		},
	});
	await page.goto("/chat");
	await expect(page.getByText("first prompt")).toBeVisible();

	await page.evaluate(() => {
		const sockets = (
			window as typeof window & {
				__mockWebSockets?: Array<{
					onmessage: ((ev: { data: string }) => unknown) | null;
				}>;
			}
		).__mockWebSockets;
		const frame = {
			data: JSON.stringify({
				type: "session_message",
				agent_id: "agent_assistant",
				session_id: "s1",
			}),
		};
		for (const ws of sockets ?? []) {
			ws.onmessage?.(frame);
			ws.onmessage?.(frame);
		}
	});

	await expect(page.getByText("new streamed message")).toBeVisible();
	await page.waitForTimeout(200);
	await expect(page.getByText("new streamed message")).toBeVisible();
});
