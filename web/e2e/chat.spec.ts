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
	await page.goto("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
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
	await page.goto("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
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
	await page.goto("/agents/assistant/chat");
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

test("a streamed tool call and its result share one block", async ({
	page,
}) => {
	let ran = false;
	const call = (fields: Record<string, unknown>) =>
		`[tool]${JSON.stringify({ name: "exec", args: { command: "gh repo view" }, ...fields })}`;
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
		agent_run: () => {
			ran = true;
			return {
				stream: [
					call({ id: "call_1" }),
					call({ id: "call_1", result: "public", done: true }),
					call({ id: "call_2" }),
				],
				result: "",
			};
		},
		config_validate: [],
		// Hold the reload that follows the run so the streamed blocks stay.
		session_messages: () => (ran ? new Promise(() => {}) : []),
	});
	await page.goto("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await input.fill("check visibility");
	await input.press("Enter");

	const blocks = page.locator("details").filter({
		has: page.locator("summary", { hasText: "exec" }),
	});
	await expect(blocks).toHaveCount(2);
	await blocks.nth(0).locator("summary").click();
	await expect(blocks.nth(0)).toContainText("public");
	await expect(blocks.nth(0)).not.toContainText("Running…");
	await blocks.nth(1).locator("summary").click();
	await expect(blocks.nth(1)).toContainText("Running…");
});

test("messages sent while a reply streams are added at once", async ({
	page,
}) => {
	const base = [
		{
			id: "m1",
			session_id: "s1",
			role: "user",
			content: "first prompt",
			timestamp: "2026-03-12T12:00:00Z",
		},
	];
	const sent: string[] = [];
	let releaseFirst = () => {};
	const firstHeld = new Promise<void>((resolve) => {
		releaseFirst = resolve;
	});
	let firstDone = false;
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
		agent_run: async (args?: Record<string, unknown>) => {
			const message = String(args?.message ?? "");
			sent.push(message);
			if (message === "message one") {
				await firstHeld;
				firstDone = true;
				return { stream: ["reply to one"], result: "" };
			}
			return { stream: [`reply to ${message.split(" ")[1]}`], result: "" };
		},
		session_messages: () => {
			if (!firstDone) return base;
			return [
				...base,
				...["one", "two", "three"].flatMap((n, i) => [
					{
						id: `u-${n}`,
						session_id: "s1",
						role: "user",
						content: `message ${n}`,
						timestamp: `2026-03-12T12:01:0${i * 2}Z`,
					},
					{
						id: `a-${n}`,
						session_id: "s1",
						role: "assistant",
						content: `reply to ${n}`,
						timestamp: `2026-03-12T12:01:0${i * 2 + 1}Z`,
					},
				]),
			];
		},
	});
	await page.goto("/agents/assistant/chat");
	await expect(page.getByText("first prompt")).toBeVisible();

	const input = page.getByPlaceholder("Describe a task or ask a question");
	const sendButton = page.getByRole("button", { name: "Send message" });
	await input.fill("message one");
	await input.press("Enter");
	await expect(page.getByText("message one")).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Stop response" }),
	).toBeVisible();

	// The first reply is still running; the composer stays usable.
	await expect(input).toBeEnabled();
	await input.fill("message two");
	await expect(sendButton).toBeEnabled();
	await input.press("Enter");
	await input.fill("message three");
	await sendButton.click();

	await expect
		.poll(() => sent)
		.toEqual(["message one", "message two", "message three"]);
	await expect(page.getByText("reply to two")).toBeVisible();
	await expect(page.getByText("reply to three")).toBeVisible();
	await expect(page.getByText(/queue|send now/i)).toHaveCount(0);
	const tops = await Promise.all(
		["message one", "message two", "message three"].map(async (t) => {
			const box = await page.getByText(t, { exact: true }).boundingBox();
			return box?.y ?? -1;
		}),
	);
	expect(tops[0]).toBeGreaterThan(0);
	expect(tops[1]).toBeGreaterThan(tops[0]);
	expect(tops[2]).toBeGreaterThan(tops[1]);

	releaseFirst();
	await expect(page.getByText("reply to one")).toBeVisible();
	for (const t of ["message one", "message two", "message three"]) {
		await expect(page.getByText(t, { exact: true })).toHaveCount(1);
	}
	for (const t of ["reply to one", "reply to two", "reply to three"]) {
		await expect(page.getByText(t, { exact: true })).toHaveCount(1);
	}
});
