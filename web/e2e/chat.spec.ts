import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { expect, test } from "./helpers/test";

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

/** Reads the composer's caret offset. */
function caret(input: import("@playwright/test").Locator) {
	return input.evaluate((el: HTMLTextAreaElement) => el.selectionStart);
}

test("up and down move between explicit lines before recalling history", async ({
	page,
}) => {
	await page.goto("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await expect(input).toBeEnabled();
	const draft = "line one\nline two\nline three";
	await input.fill(draft);

	// On the middle line both arrows move the caret and keep the text.
	await input.evaluate((el: HTMLTextAreaElement) =>
		el.setSelectionRange(12, 12),
	);
	await input.press("ArrowUp");
	await expect(input).toHaveValue(draft);
	expect(await caret(input)).toBeLessThan(9);
	await input.evaluate((el: HTMLTextAreaElement) =>
		el.setSelectionRange(12, 12),
	);
	await input.press("ArrowDown");
	await expect(input).toHaveValue(draft);
	expect(await caret(input)).toBeGreaterThan(17);

	// Down on the last line does nothing before history is entered.
	await input.press("ArrowDown");
	await expect(input).toHaveValue(draft);

	// Up on the first line steps back through history.
	await input.evaluate((el: HTMLTextAreaElement) => el.setSelectionRange(3, 3));
	await input.press("ArrowUp");
	await expect(input).toHaveValue("second prompt");
	await input.press("ArrowUp");
	await expect(input).toHaveValue("first prompt");

	// Down on the last line steps forward, ending at the kept draft.
	await input.press("ArrowDown");
	await expect(input).toHaveValue("second prompt");
	await input.press("ArrowDown");
	await expect(input).toHaveValue(draft);
	expect(await caret(input)).toBe(draft.length);
});

test("up and down treat soft-wrapped lines like explicit lines", async ({
	page,
}) => {
	await page.goto("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await expect(input).toBeEnabled();
	const draft = "word ".repeat(100).trim();
	await input.fill(draft);
	// The single line wraps onto at least three visual lines.
	const lines = await input.evaluate((el: HTMLTextAreaElement) => {
		const style = getComputedStyle(el);
		const padding =
			Number.parseFloat(style.paddingTop) +
			Number.parseFloat(style.paddingBottom);
		return (el.scrollHeight - padding) / Number.parseFloat(style.lineHeight);
	});
	expect(lines).toBeGreaterThanOrEqual(3);

	// From the end, on the last visual line, Up moves the caret up a line.
	await input.press("ArrowUp");
	await expect(input).toHaveValue(draft);
	const middle = await caret(input);
	expect(middle).toBeLessThan(draft.length);

	// On a middle visual line both arrows move the caret.
	await input.evaluate(
		(el: HTMLTextAreaElement, at) => el.setSelectionRange(at, at),
		Math.floor(draft.length / 2),
	);
	await input.press("ArrowDown");
	await expect(input).toHaveValue(draft);
	expect(await caret(input)).toBeGreaterThan(Math.floor(draft.length / 2));
	await input.evaluate(
		(el: HTMLTextAreaElement, at) => el.setSelectionRange(at, at),
		Math.floor(draft.length / 2),
	);
	await input.press("ArrowUp");
	await expect(input).toHaveValue(draft);
	expect(await caret(input)).toBeLessThan(Math.floor(draft.length / 2));

	// Only the first visual line recalls history, and only the last returns.
	await input.evaluate((el: HTMLTextAreaElement) => el.setSelectionRange(2, 2));
	await input.press("ArrowUp");
	await expect(input).toHaveValue("second prompt");
	await input.press("ArrowDown");
	await expect(input).toHaveValue(draft);
	await input.evaluate((el: HTMLTextAreaElement) => el.setSelectionRange(2, 2));
	await input.press("ArrowDown");
	await expect(input).toHaveValue(draft);
	expect(await caret(input)).toBeGreaterThan(2);
});

test("modifiers, IME composition, and selections never recall history", async ({
	page,
}) => {
	await page.goto("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await expect(input).toBeEnabled();
	await input.fill("draft");

	for (const key of [
		"Shift+ArrowUp",
		"Alt+ArrowUp",
		"Meta+ArrowUp",
		"Control+ArrowUp",
	]) {
		await input.evaluate((el: HTMLTextAreaElement) =>
			el.setSelectionRange(0, 0),
		);
		await input.press(key);
		await expect(input, key).toHaveValue("draft");
	}

	await input.evaluate((el: HTMLTextAreaElement) => el.setSelectionRange(0, 0));
	await input.evaluate((el) =>
		el.dispatchEvent(
			new KeyboardEvent("keydown", {
				key: "ArrowUp",
				isComposing: true,
				bubbles: true,
				cancelable: true,
			}),
		),
	);
	await expect(input).toHaveValue("draft");

	await input.evaluate((el: HTMLTextAreaElement) => el.setSelectionRange(0, 5));
	await input.press("ArrowUp");
	await expect(input).toHaveValue("draft");

	// Once in history, modified Down keeps the recalled entry too.
	await input.press("End");
	await input.press("ArrowUp");
	await expect(input).toHaveValue("second prompt");
	for (const key of ["Shift+ArrowDown", "Alt+ArrowDown", "Control+ArrowDown"]) {
		await input.press("End");
		await input.press(key);
		await expect(input, key).toHaveValue("second prompt");
	}

	// An empty composer recalls history from Up.
	await input.fill("");
	await input.press("ArrowUp");
	await expect(input).toHaveValue("second prompt");
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

	const input = page.getByPlaceholder("Describe a task or ask a question");
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

	const sent = page
		.getByTestId("chat-message-content")
		.filter({ hasText: "line two" });
	await expect(sent).toBeVisible();
	expect(await sent.innerText()).toBe("line one\nline two");
});

test("a sent user message renders its fenced code block as Markdown", async ({
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
		session_messages: () =>
			runs.map((run, i) => ({
				id: `r${i}`,
				session_id: "s1",
				role: "user",
				content: run.message,
				timestamp: "2026-03-12T12:00:01Z",
			})),
		agent_run: (args?: Record<string, unknown>) => {
			runs.push(args ?? {});
			return { stream: ["got it"], result: "" };
		},
	});
	await page.goto("/agents/assistant/chat");

	const longLine = `fmt.Println("${"wide ".repeat(60)}")`;
	const message = [
		"Here is the output:",
		"```go",
		"func main() {",
		`    ${longLine}`,
		"}",
		"```",
	].join("\n");
	const input = page.getByPlaceholder("Describe a task or ask a question");
	await expect(input).toBeEnabled();
	await input.fill(message);
	await input.press("Enter");
	await expect.poll(() => runs.length).toBe(1);

	const bubble = page
		.getByTestId("chat-message-bubble")
		.filter({ hasText: "Here is the output:" });
	const code = bubble.locator("pre > code.language-go");
	await expect(code).toBeVisible();
	await expect(bubble).not.toContainText("```");
	expect(await code.textContent()).toBe(`func main() {\n    ${longLine}\n}\n`);

	// The long line scrolls inside the block instead of widening the bubble.
	const pre = await bubble.locator("pre").evaluate((el) => ({
		client: el.clientWidth,
		scroll: el.scrollWidth,
	}));
	expect(pre.scroll).toBeGreaterThan(pre.client);
	const widths = await bubble.evaluate((el) => ({
		bubble: el.getBoundingClientRect().width,
		column: el.parentElement?.getBoundingClientRect().width ?? 0,
	}));
	expect(widths.bubble).toBeLessThanOrEqual(widths.column * 0.85 + 1);
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

// #740: tool entries are keyed by tool-use ID from stream to history.
const jobList = (fields: Record<string, unknown>) =>
	`[tool]${JSON.stringify({ name: "project_job_list", ...fields })}`;
const filterCall = { id: "call_a", args: { limit: 10, query: "filter" } };
const indicatorCall = { id: "call_b", args: { limit: 10, query: "indicator" } };

function mockToolChat(
	page: import("@playwright/test").Page,
	stream: string[],
	sessionMessages: () => unknown,
) {
	return mockMCP(page, {
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
		agent_run: () => ({ stream, result: "" }),
		config_validate: [],
		session_messages: sessionMessages,
	});
}

async function sendChat(page: import("@playwright/test").Page) {
	await page.goto("/agents/assistant/chat");
	const input = page.getByPlaceholder("Describe a task or ask a question");
	await input.fill("hi");
	await input.press("Enter");
}

function jobListBlocks(page: import("@playwright/test").Page) {
	return page.locator("details").filter({
		has: page.locator("summary", { hasText: "project_job_list" }),
	});
}

test("parallel calls to the same tool show one block each", async ({
	page,
}) => {
	let ran = false;
	await mockToolChat(
		page,
		[
			jobList(filterCall),
			jobList(indicatorCall),
			jobList({ ...filterCall, result: '{"total": 0}', done: true }),
			jobList({ ...indicatorCall, result: '{"total": 1}', done: true }),
		],
		() => {
			if (!ran) {
				ran = true;
				return [];
			}
			return new Promise(() => {});
		},
	);
	await sendChat(page);

	const blocks = jobListBlocks(page);
	await expect(blocks).toHaveCount(2);
	await blocks.nth(0).locator("summary").click();
	await expect(blocks.nth(0)).toContainText('"filter"');
	await expect(blocks.nth(0)).toContainText('"total": 0');
	await blocks.nth(1).locator("summary").click();
	await expect(blocks.nth(1)).toContainText('"indicator"');
	await expect(blocks.nth(1)).toContainText('"total": 1');
	await expect(page.getByText("Running…")).toHaveCount(0);
});

test("a re-delivered tool event does not add a block", async ({ page }) => {
	let ran = false;
	const result = { ...filterCall, result: '{"total": 0}', done: true };
	await mockToolChat(
		page,
		[
			jobList(filterCall),
			jobList(result),
			jobList(filterCall),
			jobList(result),
			jobList(indicatorCall),
			jobList(indicatorCall),
		],
		() => {
			if (!ran) {
				ran = true;
				return [];
			}
			return new Promise(() => {});
		},
	);
	await sendChat(page);

	const blocks = jobListBlocks(page);
	await expect(blocks).toHaveCount(2);
	await blocks.nth(0).locator("summary").click();
	await expect(blocks.nth(0)).toContainText('"total": 0');
	await expect(blocks.nth(0)).not.toContainText("Running…");
	await blocks.nth(1).locator("summary").click();
	await expect(blocks.nth(1)).toContainText("Running…");
});

test("reloading after a turn shows the same tool blocks as the live view", async ({
	page,
}) => {
	let loads = 0;
	const record = (id: string, call: typeof filterCall, result: string) => ({
		id,
		role: "tool",
		content: JSON.stringify({ name: "project_job_list", ...call, result }),
		timestamp: "2026-03-12T12:00:01Z",
	});
	const stored = [
		{
			id: "m1",
			role: "user",
			content: "hi",
			timestamp: "2026-03-12T12:00:00Z",
		},
		record("m2", filterCall, '{"total": 0}'),
		// The same call stored twice is still one call.
		record("m3", filterCall, '{"total": 0}'),
		record("m4", indicatorCall, '{"total": 1}'),
		{
			id: "m5",
			role: "assistant",
			content: "Here's the board.",
			timestamp: "2026-03-12T12:00:02Z",
		},
	];
	await mockToolChat(
		page,
		[
			jobList(filterCall),
			jobList(indicatorCall),
			jobList({ ...filterCall, result: '{"total": 0}', done: true }),
			jobList({ ...indicatorCall, result: '{"total": 1}', done: true }),
			"Here's the board.",
		],
		() => (loads++ === 0 ? [] : stored),
	);
	await sendChat(page);

	const blocks = jobListBlocks(page);
	const reply = page.getByText("Here's the board.");
	await expect.poll(() => loads).toBeGreaterThan(1);
	await expect(reply).toHaveCount(1);
	await expect(blocks).toHaveCount(2);

	await page.reload();
	await expect(reply).toHaveCount(1);
	await expect(blocks).toHaveCount(2);
	await blocks.nth(0).locator("summary").click();
	await expect(blocks.nth(0)).toContainText('"filter"');
	await blocks.nth(1).locator("summary").click();
	await expect(blocks.nth(1)).toContainText('"indicator"');
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

test("a failed turn's error attaches to its message and its icon Retry re-runs it", async ({
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
	const sent = page
		.getByTestId("chat-user-message")
		.filter({ hasText: "summarize" });
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

	// Retry is a compact icon button in the footer, next to the time and model.
	const retry = error.getByRole("button", { name: "Retry" });
	await expect(retry).toBeVisible();
	await expect(retry).toHaveText("");
	await expect(retry).toHaveAttribute("title", "Retry this message");
	await expect(retry.locator("svg")).toBeVisible();
	const retryBox = await retry.boundingBox();
	const footerBox = await error.getByText("claude/opus").boundingBox();
	expect(retryBox && footerBox).toBeTruthy();
	if (retryBox && footerBox) {
		expect(retryBox.width).toBeLessThanOrEqual(24);
		expect(retryBox.height).toBeLessThanOrEqual(24);
		expect(retryBox.x).toBeGreaterThan(footerBox.x);
		expect(
			Math.abs(
				retryBox.y + retryBox.height / 2 - (footerBox.y + footerBox.height / 2),
			),
		).toBeLessThan(4);
	}

	// Retrying while the provider still fails updates the error in place.
	await retry.click();
	await expect.poll(() => runs.length).toBe(2);
	expect(runs[1]).toMatchObject({ message: "summarize", retry: true });
	await expect(error).toContainText("502 Bad Gateway");
	await expect(page.getByTestId("chat-message-error")).toHaveCount(1);

	// Once it recovers, Retry lands the reply and the error is gone.
	failing = false;
	await sent.getByRole("button", { name: "Retry" }).click();
	await expect.poll(() => runs.length).toBe(3);
	expect(runs[2]).toMatchObject({ message: "summarize", retry: true });
	await expect(page.getByText("all good")).toBeVisible();
	await expect(page.getByTestId("chat-message-error")).toHaveCount(0);
	await expect(page.getByText("summarize", { exact: true })).toHaveCount(1);
});

// #770: the waiting indicator shows what the turn is doing.
function holdReload() {
	let ran = false;
	return () => {
		if (!ran) {
			ran = true;
			return [];
		}
		return new Promise(() => {});
	};
}

test("the waiting indicator is three staggered dots while the agent thinks", async ({
	page,
}) => {
	await mockMCP(page, {
		agent_list: [
			{ id: "a1", name: "assistant", model: "claude/opus", state: "idle" },
		],
		session_list: [
			{ id: "s1", agent_id: "a1", name: "main", created_at: "2026-03-12" },
		],
		agent_run: () => new Promise(() => {}),
		config_validate: [],
		session_messages: [],
	});
	await sendChat(page);

	const indicator = page.getByTestId("chat-activity");
	await expect(indicator).toHaveAttribute("aria-live", "polite");
	// Only the dots show; the status text is for screen readers.
	await expect(indicator).toHaveText("Agent is thinking");
	await expect(indicator.locator(".sr-only")).toHaveText("Agent is thinking");
	const dots = page.getByTestId("chat-activity-dot");
	await expect(dots).toHaveCount(3);
	const animations = await dots.evaluateAll((els) =>
		els.map((el) => {
			const style = getComputedStyle(el);
			return {
				name: style.animationName,
				delay: style.animationDelay,
				iterations: style.animationIterationCount,
			};
		}),
	);
	for (const animation of animations) {
		expect(animation.name).toMatch(/chat-activity-wave/);
		expect(animation.iterations).toBe("infinite");
	}
	// Staggered, not blinking in unison.
	expect(animations.map((a) => a.delay)).toEqual(["0s", "0.16s", "0.32s"]);
});

test("the waiting indicator dots are static with reduced motion", async ({
	page,
}) => {
	await page.emulateMedia({ reducedMotion: "reduce" });
	await mockMCP(page, {
		agent_list: [
			{ id: "a1", name: "assistant", model: "claude/opus", state: "idle" },
		],
		session_list: [
			{ id: "s1", agent_id: "a1", name: "main", created_at: "2026-03-12" },
		],
		agent_run: () => new Promise(() => {}),
		config_validate: [],
		session_messages: [],
	});
	await sendChat(page);

	await expect(page.getByTestId("chat-activity-dot")).toHaveCount(3);
	await expect(
		page
			.getByTestId("chat-activity-dot")
			.evaluateAll((els) =>
				els.map((el) => getComputedStyle(el).animationName),
			),
	).resolves.toEqual(["none", "none", "none"]);
});

test("a running tool spins and counts up its elapsed time on its chip", async ({
	page,
}) => {
	await page.clock.install();
	await mockToolChat(page, [jobList(filterCall)], holdReload());
	await sendChat(page);

	const block = jobListBlocks(page);
	await expect(block.getByTestId("tool-call-spinner")).toBeVisible();
	// The tool's chip is its status; no typing bubble repeats it below.
	await expect(page.getByTestId("chat-activity")).toHaveCount(0);
	const elapsed = block.getByTestId("tool-call-elapsed");
	await expect(elapsed).toHaveCount(0);

	await page.clock.fastForward(6000);
	await expect(elapsed).toHaveText(/· [67]s/);
	await page.clock.fastForward(4000);
	await expect(elapsed).toHaveText(/· 1[01]s/);
});

test("finished tool chips show a check or a warning with the error", async ({
	page,
}) => {
	await mockToolChat(
		page,
		[
			jobList(filterCall),
			jobList({ ...filterCall, result: '{"total": 0}', done: true }),
			jobList(indicatorCall),
			jobList({ ...indicatorCall, error: "timed out", done: true }),
		],
		holdReload(),
	);
	await sendChat(page);

	const blocks = jobListBlocks(page);
	await expect(blocks).toHaveCount(2);
	await expect(blocks.nth(0).getByTestId("tool-call-check")).toBeVisible();
	await expect(blocks.nth(1).getByTestId("tool-call-warning")).toBeVisible();
	await expect(blocks.nth(1).locator("summary")).toHaveAttribute(
		"title",
		"timed out",
	);
	await expect(page.getByTestId("tool-call-spinner")).toHaveCount(0);
	// Between tools and before any reply, the turn is thinking again.
	await expect(
		page.getByTestId("chat-activity").locator(".sr-only"),
	).toHaveText("Agent is thinking");
});

test("the waiting indicator goes away once reply text streams", async ({
	page,
}) => {
	await mockToolChat(
		page,
		[
			jobList(filterCall),
			jobList({ ...filterCall, result: '{"total": 0}', done: true }),
			"Here is the answer",
		],
		holdReload(),
	);
	await sendChat(page);

	await expect(page.getByText("Here is the answer")).toBeVisible();
	await expect(page.getByTestId("chat-activity")).toHaveCount(0);
});

test("user bubbles stay docked right with their text left-aligned", async ({
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
		config_validate: [],
		session_messages: [
			{
				id: "m1",
				session_id: "s1",
				role: "user",
				content: "a much longer first line of text\nshort",
				timestamp: "2026-03-12T12:00:00Z",
			},
		],
	});
	await page.goto("/agents/assistant/chat");

	const text = page
		.getByTestId("chat-user-message")
		.getByTestId("chat-message-content")
		.filter({ hasText: "short" });
	await expect(text).toBeVisible();
	expect(["left", "start"]).toContain(
		await text.evaluate((el) => getComputedStyle(el).textAlign),
	);

	// Each line starts at the bubble's left padding edge.
	const bubble = text.locator("xpath=..");
	const [bubbleBox, textBox] = await Promise.all([
		bubble.boundingBox(),
		text.boundingBox(),
	]);
	const padding = await bubble.evaluate((el) =>
		Number.parseFloat(getComputedStyle(el).paddingLeft),
	);
	expect(textBox?.x).toBeCloseTo((bubbleBox?.x ?? 0) + padding, 0);

	// The bubble itself still sits on the right side of the chat.
	const row = bubble.locator("xpath=..");
	const rowBox = await row.boundingBox();
	expect((bubbleBox?.x ?? 0) + (bubbleBox?.width ?? 0)).toBeCloseTo(
		(rowBox?.x ?? 0) + (rowBox?.width ?? 0),
		0,
	);
});

test("links in chat messages open in a new tab", async ({ page, context }) => {
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
		session_messages: [
			{
				id: "m1",
				session_id: "s1",
				role: "user",
				content: "read https://example.com/user please",
				timestamp: "2026-03-12T12:00:00Z",
			},
			{
				id: "m2",
				session_id: "s1",
				role: "assistant",
				content:
					"See [the docs](https://example.com/docs) or https://example.com/bare",
				timestamp: "2026-03-12T12:00:01Z",
			},
		],
	});
	await context.route("https://example.com/**", (route) =>
		route.fulfill({ contentType: "text/html", body: "<p>external</p>" }),
	);
	await page.goto("/agents/assistant/chat");

	const links = [
		page.getByTestId("chat-user-message").getByRole("link", {
			name: "https://example.com/user",
		}),
		page.getByRole("link", { name: "the docs" }),
		page.getByRole("link", { name: "https://example.com/bare" }),
	];
	for (const link of links) {
		await expect(link).toHaveAttribute("target", "_blank");
		await expect(link).toHaveAttribute("rel", "noopener noreferrer");
	}

	const chatURL = page.url();
	const popup = context.waitForEvent("page");
	await links[1].click();
	await expect((await popup).locator("p")).toHaveText("external");
	expect(page.url()).toBe(chatURL);
});

test("channel file attachments show their placeholder, not a broken link", async ({
	page,
}) => {
	const png =
		"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";
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
		session_messages: [
			{
				id: "m1",
				session_id: "s1",
				role: "user",
				content:
					"what is in this export?\n[file attached: chrome-net-export-log.json (application/json, 1.2 MB) saved at /data/media/incoming/signal/1_chrome-net-export-log.json]",
				media_urls: [
					png,
					"file:///data/media/incoming/signal/1_chrome-net-export-log.json",
				],
				timestamp: "2026-03-12T12:00:00Z",
			},
		],
	});
	await page.goto("/agents/assistant/chat");

	const message = page.getByTestId("chat-user-message");
	await expect(message).toContainText(
		"[file attached: chrome-net-export-log.json (application/json, 1.2 MB)",
	);
	await expect(message.locator("img")).toHaveCount(1);
	await expect(message.getByText("Open attachment")).toHaveCount(0);
	await expect(message.locator('a[href^="file:"]')).toHaveCount(0);
});
