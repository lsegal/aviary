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

test("Shift+Enter inserts a newline and Enter sends it with its line breaks", async ({
	page,
}) => {
	const runs: Record<string, unknown>[] = [];
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
		session_messages: () => [
			{
				id: "m1",
				session_id: "s1",
				role: "user",
				content: "first prompt",
				timestamp: "2026-03-12T12:00:00Z",
			},
			...runs.map((run, i) => ({
				id: `r${i}`,
				session_id: "s1",
				role: "user",
				content: run.message,
				timestamp: "2026-03-12T12:00:01Z",
			})),
		],
		agent_run: (args?: Record<string, unknown>) => {
			runs.push(args ?? {});
			return { stream: ["got it"], result: "" };
		},
	});
	await page.goto("/agents/assistant/chat");

	const input = page.getByPlaceholder("Type a message or paste an image…");
	await expect(input).toBeEnabled();
	await input.fill("line oneline two");
	const before = (await input.boundingBox())?.height ?? 0;

	// The newline goes in at the caret, not at the end.
	await input.evaluate((el: HTMLTextAreaElement) => el.setSelectionRange(8, 8));
	await input.press("Shift+Enter");
	await expect(input).toHaveValue("line one\nline two");
	await expect
		.poll(async () => (await input.boundingBox())?.height ?? 0)
		.toBeGreaterThan(before);

	// With the caret below the first line, ArrowUp moves it instead of
	// recalling history.
	await input.press("ArrowUp");
	await expect(input).toHaveValue("line one\nline two");

	// Enter during an IME composition only confirms the composition.
	await input.evaluate((el) =>
		el.dispatchEvent(
			new KeyboardEvent("keydown", {
				key: "Enter",
				isComposing: true,
				bubbles: true,
				cancelable: true,
			}),
		),
	);
	await expect(input).toHaveValue("line one\nline two");

	await input.press("Enter");
	await expect.poll(() => runs.length).toBe(1);
	expect(runs[0].message).toBe("line one\nline two");
	await expect(input).toHaveValue("");

	const sent = page.locator(".whitespace-pre-wrap", { hasText: "line two" });
	await expect(sent).toBeVisible();
	expect(await sent.innerText()).toBe("line one\nline two");
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

	const input = page.getByPlaceholder("Type a message or paste an image…");
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

	const input = page.getByPlaceholder("Type a message or paste an image…");
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

const RED_PNG =
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg==";
const BLUE_PNG =
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPj/HwADBwIAMCbHYQAAAABJRU5ErkJggg==";

/** Pastes PNG files (base64) into the element the way a browser paste does. */
async function pasteImages(
	input: import("@playwright/test").Locator,
	pngs: string[],
) {
	await input.evaluate((el, images) => {
		const data = new DataTransfer();
		images.forEach((b64, i) => {
			const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
			data.items.add(
				new File([bytes], `pasted-${i}.png`, { type: "image/png" }),
			);
		});
		el.dispatchEvent(
			new ClipboardEvent("paste", {
				clipboardData: data,
				bubbles: true,
				cancelable: true,
			}),
		);
	}, pngs);
}

function chatMocks(runs: Record<string, unknown>[]) {
	return {
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
		agent_run: (args?: Record<string, unknown>) => {
			runs.push(args ?? {});
			return { stream: ["got them"], result: "" };
		},
		config_validate: [],
		// What the server persists once a prompt has run.
		session_messages: () =>
			runs.flatMap((run, i) => [
				{
					id: `u${i}`,
					session_id: "s1",
					role: "user",
					content: run.message,
					media_urls: run.media_urls,
					timestamp: "2026-03-12T12:00:00Z",
				},
				{
					id: `a${i}`,
					session_id: "s1",
					role: "assistant",
					content: "got them",
					timestamp: "2026-03-12T12:00:01Z",
				},
			]),
	};
}

test("pasting two images attaches and sends both", async ({ page }) => {
	const runs: Record<string, unknown>[] = [];
	await mockMCP(page, chatMocks(runs));
	await page.goto("/agents/assistant/chat");

	const input = page.getByPlaceholder("Type a message or paste an image…");
	await expect(input).toBeEnabled();
	await pasteImages(input, [RED_PNG]);
	await pasteImages(input, [BLUE_PNG]);

	const previews = page.getByTestId("chat-attachment");
	await expect(previews).toHaveCount(2);
	await expect(
		page.getByRole("button", { name: "Remove image 1" }),
	).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Remove image 2" }),
	).toBeVisible();

	await input.fill("compare these");
	await input.press("Enter");

	await expect(previews).toHaveCount(0);
	await expect.poll(() => runs.length).toBe(1);
	expect(runs[0].message).toBe("compare these");
	expect(runs[0].media_urls).toEqual([
		`data:image/png;base64,${RED_PNG}`,
		`data:image/png;base64,${BLUE_PNG}`,
	]);

	// The sent bubble, reloaded from the persisted session, shows both.
	await expect(page.getByText("got them")).toBeVisible();
	const bubble = page
		.locator(".bg-blue-600")
		.filter({ hasText: "compare these" });
	await expect(bubble.locator("img")).toHaveCount(2);
	await page.reload();
	await expect(bubble.locator("img")).toHaveCount(2);
	await expect(bubble.locator("img").nth(1)).toHaveAttribute(
		"src",
		`data:image/png;base64,${BLUE_PNG}`,
	);
});

test("removing one attachment keeps the others", async ({ page }) => {
	const runs: Record<string, unknown>[] = [];
	await mockMCP(page, chatMocks(runs));
	await page.goto("/agents/assistant/chat");

	const input = page.getByPlaceholder("Type a message or paste an image…");
	await expect(input).toBeEnabled();
	// One paste carrying two images adds both.
	await pasteImages(input, [RED_PNG, BLUE_PNG]);
	const previews = page.getByTestId("chat-attachment");
	await expect(previews).toHaveCount(2);

	await page.getByRole("button", { name: "Remove image 1" }).click();
	await expect(previews).toHaveCount(1);
	await expect(previews.locator("img")).toHaveAttribute(
		"src",
		`data:image/png;base64,${BLUE_PNG}`,
	);

	await page.getByRole("button", { name: "Send message" }).click();
	await expect.poll(() => runs.length).toBe(1);
	expect(runs[0].media_urls).toEqual([`data:image/png;base64,${BLUE_PNG}`]);
});

test("the file picker and drag-and-drop append attachments", async ({
	page,
}) => {
	await mockMCP(page, chatMocks([]));
	await page.goto("/agents/assistant/chat");

	const input = page.getByPlaceholder("Type a message or paste an image…");
	await expect(input).toBeEnabled();
	await pasteImages(input, [RED_PNG]);

	// Pick two files at once in the hidden file input.
	await page.getByTestId("chat-attachment-input").evaluate(
		(el, images) => {
			const data = new DataTransfer();
			for (const b64 of images) {
				const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
				data.items.add(new File([bytes], "picked.png", { type: "image/png" }));
			}
			(el as HTMLInputElement).files = data.files;
			el.dispatchEvent(new Event("change", { bubbles: true }));
		},
		[BLUE_PNG, RED_PNG],
	);
	const previews = page.getByTestId("chat-attachment");
	await expect(previews).toHaveCount(3);

	await input.evaluate((el, b64) => {
		const data = new DataTransfer();
		const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
		data.items.add(new File([bytes], "dropped.png", { type: "image/png" }));
		el.dispatchEvent(
			new DragEvent("drop", {
				dataTransfer: data,
				bubbles: true,
				cancelable: true,
			}),
		);
	}, BLUE_PNG);
	await expect(previews).toHaveCount(4);
});

test("the attachment limit is shown instead of dropping images silently", async ({
	page,
}) => {
	await mockMCP(page, chatMocks([]));
	await page.goto("/agents/assistant/chat");

	const input = page.getByPlaceholder("Type a message or paste an image…");
	await expect(input).toBeEnabled();
	await pasteImages(input, Array(9).fill(RED_PNG));
	await pasteImages(input, [BLUE_PNG, RED_PNG, BLUE_PNG]);

	await expect(page.getByTestId("chat-attachment")).toHaveCount(10);
	await expect(page.getByRole("status")).toHaveText(
		"A message can carry up to 10 images; 2 not attached.",
	);
});
