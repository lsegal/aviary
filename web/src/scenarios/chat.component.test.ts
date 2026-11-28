import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { reload, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP, webSockets } from "../component-test/server";

// The pages this file renders (see component-test/routes.ts).
declarePages(() => import("../views/ChatView.vue"));

// The fake server's unrouted WebSockets open and stay silent, as the spec's
// stub socket did.

const count = (loc: Locator) => loc.elements().length;
const rect = (loc: Locator) => loc.element().getBoundingClientRect();

/** Focuses `loc` and presses `key`, as Playwright's locator.press. */
async function press(loc: Locator, key: string) {
	const el = loc.element() as HTMLElement;
	if (document.activeElement !== el) el.focus();
	await commands.keyPress(key);
}

/** Moves the composer's caret, as the spec's setSelectionRange calls. */
function select(loc: Locator, start: number, end = start) {
	(loc.element() as HTMLTextAreaElement).setSelectionRange(start, end);
}

/** Reads the composer's caret offset. */
function caret(loc: Locator) {
	return (loc.element() as HTMLTextAreaElement).selectionStart;
}

/** Dispatches a keydown during an IME composition. */
function composingKey(loc: Locator, key: string) {
	loc.element().dispatchEvent(
		new KeyboardEvent("keydown", {
			key,
			isComposing: true,
			bubbles: true,
			cancelable: true,
		}),
	);
}

beforeEach(() => {
	mockMCP({
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

afterEach(() => {
	vi.useRealTimers();
});

test("up arrow recalls previous chat messages", async () => {
	await renderApp("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await expect.element(input).toBeEnabled();

	await input.fill("draft message");
	await press(input, "ArrowUp");
	await expect.element(input).toHaveValue("second prompt");

	await press(input, "ArrowUp");
	await expect.element(input).toHaveValue("first prompt");

	await press(input, "ArrowDown");
	await expect.element(input).toHaveValue("second prompt");

	await press(input, "ArrowDown");
	await expect.element(input).toHaveValue("draft message");
});

test("up and down move between explicit lines before recalling history", async () => {
	await renderApp("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await expect.element(input).toBeEnabled();
	const draft = "line one\nline two\nline three";
	await input.fill(draft);

	// On the middle line both arrows move the caret and keep the text.
	select(input, 12);
	await press(input, "ArrowUp");
	await expect.element(input).toHaveValue(draft);
	expect(caret(input)).toBeLessThan(9);
	select(input, 12);
	await press(input, "ArrowDown");
	await expect.element(input).toHaveValue(draft);
	expect(caret(input)).toBeGreaterThan(17);

	// Down on the last line does nothing before history is entered.
	await press(input, "ArrowDown");
	await expect.element(input).toHaveValue(draft);

	// Up on the first line steps back through history.
	select(input, 3);
	await press(input, "ArrowUp");
	await expect.element(input).toHaveValue("second prompt");
	await press(input, "ArrowUp");
	await expect.element(input).toHaveValue("first prompt");

	// Down on the last line steps forward, ending at the kept draft.
	await press(input, "ArrowDown");
	await expect.element(input).toHaveValue("second prompt");
	await press(input, "ArrowDown");
	await expect.element(input).toHaveValue(draft);
	expect(caret(input)).toBe(draft.length);
});

test("up and down treat soft-wrapped lines like explicit lines", async () => {
	await renderApp("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await expect.element(input).toBeEnabled();
	const draft = "word ".repeat(100).trim();
	await input.fill(draft);
	// The single line wraps onto at least three visual lines.
	const lines = ((el: Element) => {
		const style = getComputedStyle(el);
		const padding =
			Number.parseFloat(style.paddingTop) +
			Number.parseFloat(style.paddingBottom);
		return (el.scrollHeight - padding) / Number.parseFloat(style.lineHeight);
	})(input.element());
	expect(lines).toBeGreaterThanOrEqual(3);

	// From the end, on the last visual line, Up moves the caret up a line.
	await press(input, "ArrowUp");
	await expect.element(input).toHaveValue(draft);
	const middle = caret(input);
	expect(middle).toBeLessThan(draft.length);

	// On a middle visual line both arrows move the caret.
	select(input, Math.floor(draft.length / 2));
	await press(input, "ArrowDown");
	await expect.element(input).toHaveValue(draft);
	expect(caret(input)).toBeGreaterThan(Math.floor(draft.length / 2));
	select(input, Math.floor(draft.length / 2));
	await press(input, "ArrowUp");
	await expect.element(input).toHaveValue(draft);
	expect(caret(input)).toBeLessThan(Math.floor(draft.length / 2));

	// Only the first visual line recalls history, and only the last returns.
	select(input, 2);
	await press(input, "ArrowUp");
	await expect.element(input).toHaveValue("second prompt");
	await press(input, "ArrowDown");
	await expect.element(input).toHaveValue(draft);
	select(input, 2);
	await press(input, "ArrowDown");
	await expect.element(input).toHaveValue(draft);
	expect(caret(input)).toBeGreaterThan(2);
});

test("modifiers, IME composition, and selections never recall history", async () => {
	await renderApp("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await expect.element(input).toBeEnabled();
	await input.fill("draft");

	for (const key of [
		"Shift+ArrowUp",
		"Alt+ArrowUp",
		"Meta+ArrowUp",
		"Control+ArrowUp",
	]) {
		select(input, 0);
		await press(input, key);
		await expect.element(input).toHaveValue("draft");
	}

	select(input, 0);
	composingKey(input, "ArrowUp");
	await expect.element(input).toHaveValue("draft");

	select(input, 0, 5);
	await press(input, "ArrowUp");
	await expect.element(input).toHaveValue("draft");

	// Once in history, modified Down keeps the recalled entry too.
	await press(input, "End");
	await press(input, "ArrowUp");
	await expect.element(input).toHaveValue("second prompt");
	for (const key of ["Shift+ArrowDown", "Alt+ArrowDown", "Control+ArrowDown"]) {
		await press(input, "End");
		await press(input, key);
		await expect.element(input).toHaveValue("second prompt");
	}

	// An empty composer recalls history from Up.
	await input.fill("");
	await press(input, "ArrowUp");
	await expect.element(input).toHaveValue("second prompt");
});

test("Shift+Enter inserts a newline and Enter sends it with its line breaks", async () => {
	const runs: Record<string, unknown>[] = [];
	mockMCP({
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
	await renderApp("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await expect.element(input).toBeEnabled();
	await input.fill("line oneline two");
	const before = rect(input).height;

	// The newline goes in at the caret, not at the end.
	select(input, 8);
	await press(input, "Shift+Enter");
	await expect.element(input).toHaveValue("line one\nline two");
	await expect.poll(() => rect(input).height).toBeGreaterThan(before);

	// With the caret below the first line, ArrowUp moves it instead of
	// recalling history.
	await press(input, "ArrowUp");
	await expect.element(input).toHaveValue("line one\nline two");

	// Enter during an IME composition only confirms the composition.
	composingKey(input, "Enter");
	await expect.element(input).toHaveValue("line one\nline two");

	await press(input, "Enter");
	await expect.poll(() => runs.length).toBe(1);
	expect(runs[0].message).toBe("line one\nline two");
	await expect.element(input).toHaveValue("");

	const sent = page
		.getByTestId("chat-message-content")
		.filter({ hasText: "line two" });
	await expect.element(sent).toBeVisible();
	// biome-ignore lint/performance/useDomNodeTextContent: the rendered line break is the point.
	expect((sent.element() as HTMLElement).innerText).toBe("line one\nline two");
});

test("a sent user message renders its fenced code block as Markdown", async () => {
	const runs: Record<string, unknown>[] = [];
	mockMCP({
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
	await renderApp("/agents/assistant/chat");

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
	await expect.element(input).toBeEnabled();
	await input.fill(message);
	await press(input, "Enter");
	await expect.poll(() => runs.length).toBe(1);

	const bubble = page
		.getByTestId("chat-message-bubble")
		.filter({ hasText: "Here is the output:" });
	const code = bubble.locator("pre > code.language-go");
	await expect.element(code).toBeVisible();
	await expect.element(bubble).not.toMatchTextContent("```");
	expect(code.element().textContent).toBe(
		`func main() {\n    ${longLine}\n}\n`,
	);

	// The long line scrolls inside the block instead of widening the bubble.
	const preEl = bubble.locator("pre").element();
	const pre = { client: preEl.clientWidth, scroll: preEl.scrollWidth };
	expect(pre.scroll).toBeGreaterThan(pre.client);
	const bubbleEl = bubble.element();
	const widths = {
		bubble: bubbleEl.getBoundingClientRect().width,
		column: bubbleEl.parentElement?.getBoundingClientRect().width ?? 0,
	};
	expect(widths.bubble).toBeLessThanOrEqual(widths.column * 0.85 + 1);
});

test("streaming response updates without dropping existing messages", async () => {
	mockMCP({
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
	await renderApp("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await input.fill("stream this");
	await press(input, "Enter");

	await expect.element(page.getByText("first prompt")).toBeVisible();
	await expect.element(page.getByText("first response")).toBeVisible();
	await expect.element(page.getByText("second prompt")).toBeVisible();
	await expect.element(page.getByText("partial response")).toBeVisible();
});

test("stale session reloads cannot overwrite newer messages", async () => {
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
	mockMCP({
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
	await renderApp("/agents/assistant/chat");
	await expect.element(page.getByText("first prompt")).toBeVisible();

	const frame = JSON.stringify({
		type: "session_message",
		agent_id: "agent_assistant",
		session_id: "s1",
	});
	for (const ws of webSockets()) {
		ws.receive(frame);
		ws.receive(frame);
	}

	await expect.element(page.getByText("new streamed message")).toBeVisible();
	await new Promise((r) => setTimeout(r, 200));
	await expect.element(page.getByText("new streamed message")).toBeVisible();
});

test("a streamed tool call and its result share one block", async () => {
	let ran = false;
	const call = (fields: Record<string, unknown>) =>
		`[tool]${JSON.stringify({ name: "exec", args: { command: "gh repo view" }, ...fields })}`;
	mockMCP({
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
	await renderApp("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await input.fill("check visibility");
	await press(input, "Enter");

	const blocks = page.locator("details").filter({
		has: page.locator("summary").filter({ hasText: "exec" }),
	});
	await expect.poll(() => count(blocks)).toBe(2);
	await blocks.nth(0).locator("summary").click();
	await expect.element(blocks.nth(0)).toMatchTextContent("public");
	await expect.element(blocks.nth(0)).not.toMatchTextContent("Running…");
	await blocks.nth(1).locator("summary").click();
	await expect.element(blocks.nth(1)).toMatchTextContent("Running…");
});

// #740: tool entries are keyed by tool-use ID from stream to history.
const jobList = (fields: Record<string, unknown>) =>
	`[tool]${JSON.stringify({ name: "project_job_list", ...fields })}`;
const filterCall = { id: "call_a", args: { limit: 10, query: "filter" } };
const indicatorCall = { id: "call_b", args: { limit: 10, query: "indicator" } };

function mockToolChat(stream: string[], sessionMessages: () => unknown) {
	return mockMCP({
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

async function sendChat() {
	await renderApp("/agents/assistant/chat");
	const input = page.getByPlaceholder("Describe a task or ask a question");
	await input.fill("hi");
	await press(input, "Enter");
}

function jobListBlocks() {
	return page.locator("details").filter({
		has: page.locator("summary").filter({ hasText: "project_job_list" }),
	});
}

test("parallel calls to the same tool show one block each", async () => {
	let ran = false;
	mockToolChat(
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
	await sendChat();

	const blocks = jobListBlocks();
	await expect.poll(() => count(blocks)).toBe(2);
	await blocks.nth(0).locator("summary").click();
	await expect.element(blocks.nth(0)).toMatchTextContent('"filter"');
	await expect.element(blocks.nth(0)).toMatchTextContent('"total": 0');
	await blocks.nth(1).locator("summary").click();
	await expect.element(blocks.nth(1)).toMatchTextContent('"indicator"');
	await expect.element(blocks.nth(1)).toMatchTextContent('"total": 1');
	await expect.poll(() => count(page.getByText("Running…"))).toBe(0);
});

test("a re-delivered tool event does not add a block", async () => {
	let ran = false;
	const result = { ...filterCall, result: '{"total": 0}', done: true };
	mockToolChat(
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
	await sendChat();

	const blocks = jobListBlocks();
	await expect.poll(() => count(blocks)).toBe(2);
	await blocks.nth(0).locator("summary").click();
	await expect.element(blocks.nth(0)).toMatchTextContent('"total": 0');
	await expect.element(blocks.nth(0)).not.toMatchTextContent("Running…");
	await blocks.nth(1).locator("summary").click();
	await expect.element(blocks.nth(1)).toMatchTextContent("Running…");
});

test("reloading after a turn shows the same tool blocks as the live view", async () => {
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
	mockToolChat(
		[
			jobList(filterCall),
			jobList(indicatorCall),
			jobList({ ...filterCall, result: '{"total": 0}', done: true }),
			jobList({ ...indicatorCall, result: '{"total": 1}', done: true }),
			"Here's the board.",
		],
		() => (loads++ === 0 ? [] : stored),
	);
	await sendChat();

	const blocks = jobListBlocks();
	const reply = page.getByText("Here's the board.");
	await expect.poll(() => loads).toBeGreaterThan(1);
	await expect.poll(() => count(reply)).toBe(1);
	await expect.poll(() => count(blocks)).toBe(2);

	await reload();
	await expect.poll(() => count(reply)).toBe(1);
	await expect.poll(() => count(blocks)).toBe(2);
	await blocks.nth(0).locator("summary").click();
	await expect.element(blocks.nth(0)).toMatchTextContent('"filter"');
	await blocks.nth(1).locator("summary").click();
	await expect.element(blocks.nth(1)).toMatchTextContent('"indicator"');
});

test("messages sent while a reply streams are added at once", async () => {
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
	mockMCP({
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
	await renderApp("/agents/assistant/chat");
	await expect.element(page.getByText("first prompt")).toBeVisible();

	const input = page.getByPlaceholder("Describe a task or ask a question");
	const sendButton = page.getByRole("button", { name: "Send message" });
	await input.fill("message one");
	await press(input, "Enter");
	await expect.element(page.getByText("message one")).toBeVisible();
	await expect
		.element(page.getByRole("button", { name: "Stop response" }))
		.toBeVisible();

	// The first reply is still running; the composer stays usable.
	await expect.element(input).toBeEnabled();
	await input.fill("message two");
	await expect.element(sendButton).toBeEnabled();
	await press(input, "Enter");
	await input.fill("message three");
	await sendButton.click();

	await expect
		.poll(() => sent)
		.toEqual(["message one", "message two", "message three"]);
	await expect.element(page.getByText("reply to two")).toBeVisible();
	await expect.element(page.getByText("reply to three")).toBeVisible();
	await expect.poll(() => count(page.getByText(/queue|send now/i))).toBe(0);
	const tops = ["message one", "message two", "message three"].map(
		(t) => rect(page.getByText(t, { exact: true })).y,
	);
	expect(tops[0]).toBeGreaterThan(0);
	expect(tops[1]).toBeGreaterThan(tops[0]);
	expect(tops[2]).toBeGreaterThan(tops[1]);

	releaseFirst();
	await expect.element(page.getByText("reply to one")).toBeVisible();
	for (const t of ["message one", "message two", "message three"]) {
		await expect.poll(() => count(page.getByText(t, { exact: true }))).toBe(1);
	}
	for (const t of ["reply to one", "reply to two", "reply to three"]) {
		await expect.poll(() => count(page.getByText(t, { exact: true }))).toBe(1);
	}
});

test("a failed turn's error attaches to its message and its icon Retry re-runs it", async () => {
	const runs: Record<string, unknown>[] = [];
	let failing = true;
	mockMCP({
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
	await renderApp("/agents/assistant/chat");

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await expect.element(input).toBeEnabled();
	await input.fill("summarize");
	await press(input, "Enter");

	// The error hangs off the sent message's bubble, not as its own entry.
	const sent = page
		.getByTestId("chat-user-message")
		.filter({ hasText: "summarize" });
	const error = sent.getByTestId("chat-message-error");
	await expect
		.element(error)
		.toMatchTextContent("Error: MCP error: 502 Bad Gateway");
	await expect.element(error).toMatchTextContent("claude/opus");
	await expect
		.poll(() => count(page.getByTestId("chat-message-error")))
		.toBe(1);
	const bubble = rect(sent.locator(".bg-blue-600"));
	const attached = rect(error);
	// Staggered: tucked under the bubble and inset from its right edge.
	expect(attached.y).toBeLessThan(bubble.y + bubble.height + 1);
	expect(attached.x + attached.width).toBeLessThan(bubble.x + bubble.width);

	// Retry is a compact icon button in the footer, next to the time and model.
	const retry = error.getByRole("button", { name: "Retry" });
	await expect.element(retry).toBeVisible();
	await expect.element(retry).toHaveText("");
	await expect.element(retry).toHaveAttribute("title", "Retry this message");
	await expect.element(retry.locator("svg")).toBeVisible();
	const retryBox = rect(retry);
	const footerBox = rect(error.getByText("claude/opus"));
	expect(retryBox.width).toBeLessThanOrEqual(24);
	expect(retryBox.height).toBeLessThanOrEqual(24);
	expect(retryBox.x).toBeGreaterThan(footerBox.x);
	expect(
		Math.abs(
			retryBox.y + retryBox.height / 2 - (footerBox.y + footerBox.height / 2),
		),
	).toBeLessThan(4);

	// Retrying while the provider still fails updates the error in place.
	await retry.click();
	await expect.poll(() => runs.length).toBe(2);
	expect(runs[1]).toMatchObject({ message: "summarize", retry: true });
	await expect.element(error).toMatchTextContent("502 Bad Gateway");
	await expect
		.poll(() => count(page.getByTestId("chat-message-error")))
		.toBe(1);

	// Once it recovers, Retry lands the reply and the error is gone.
	failing = false;
	await sent.getByRole("button", { name: "Retry" }).click();
	await expect.poll(() => runs.length).toBe(3);
	expect(runs[2]).toMatchObject({ message: "summarize", retry: true });
	await expect.element(page.getByText("all good")).toBeVisible();
	await expect
		.poll(() => count(page.getByTestId("chat-message-error")))
		.toBe(0);
	await expect
		.poll(() => count(page.getByText("summarize", { exact: true })))
		.toBe(1);
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

test("the waiting indicator is three staggered dots while the agent thinks", async () => {
	mockMCP({
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
	await sendChat();

	const indicator = page.getByTestId("chat-activity");
	await expect.element(indicator).toHaveAttribute("aria-live", "polite");
	// Only the dots show; the status text is for screen readers.
	await expect.element(indicator).toHaveText("Agent is thinking");
	await expect
		.element(indicator.locator(".sr-only"))
		.toHaveText("Agent is thinking");
	const dots = page.getByTestId("chat-activity-dot");
	await expect.poll(() => count(dots)).toBe(3);
	const animations = dots.elements().map((el) => {
		const style = getComputedStyle(el);
		return {
			name: style.animationName,
			delay: style.animationDelay,
			iterations: style.animationIterationCount,
		};
	});
	for (const animation of animations) {
		expect(animation.name).toMatch(/chat-activity-wave/);
		expect(animation.iterations).toBe("infinite");
	}
	// Staggered, not blinking in unison.
	expect(animations.map((a) => a.delay)).toEqual(["0s", "0.16s", "0.32s"]);
});

test("the waiting indicator dots are static with reduced motion", async () => {
	await commands.emulateMedia({ reducedMotion: "reduce" });
	mockMCP({
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
	await sendChat();

	await expect.poll(() => count(page.getByTestId("chat-activity-dot"))).toBe(3);
	expect(
		page
			.getByTestId("chat-activity-dot")
			.elements()
			.map((el) => getComputedStyle(el).animationName),
	).toEqual(["none", "none", "none"]);
});

test("a running tool spins and counts up its elapsed time on its chip", async () => {
	// Only the clock and the app's tick are faked; timeouts stay real so the
	// app and the fake server keep running.
	vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
	mockToolChat([jobList(filterCall)], holdReload());
	await sendChat();

	const block = jobListBlocks();
	await expect.element(block.getByTestId("tool-call-spinner")).toBeVisible();
	// The tool's chip is its status; no typing bubble repeats it below.
	await expect.poll(() => count(page.getByTestId("chat-activity"))).toBe(0);
	const elapsed = block.getByTestId("tool-call-elapsed");
	await expect.poll(() => count(elapsed)).toBe(0);

	await vi.advanceTimersByTimeAsync(6000);
	await expect.element(elapsed).toHaveText(/· [67]s/);
	await vi.advanceTimersByTimeAsync(4000);
	await expect.element(elapsed).toHaveText(/· 1[01]s/);
});

test("finished tool chips show a check or a warning with the error", async () => {
	mockToolChat(
		[
			jobList(filterCall),
			jobList({ ...filterCall, result: '{"total": 0}', done: true }),
			jobList(indicatorCall),
			jobList({ ...indicatorCall, error: "timed out", done: true }),
		],
		holdReload(),
	);
	await sendChat();

	const blocks = jobListBlocks();
	await expect.poll(() => count(blocks)).toBe(2);
	await expect
		.element(blocks.nth(0).getByTestId("tool-call-check"))
		.toBeVisible();
	await expect
		.element(blocks.nth(1).getByTestId("tool-call-warning"))
		.toBeVisible();
	await expect
		.element(blocks.nth(1).locator("summary"))
		.toHaveAttribute("title", "timed out");
	await expect.poll(() => count(page.getByTestId("tool-call-spinner"))).toBe(0);
	// Between tools and before any reply, the turn is thinking again.
	await expect
		.element(page.getByTestId("chat-activity").locator(".sr-only"))
		.toHaveText("Agent is thinking");
});

test("the waiting indicator goes away once reply text streams", async () => {
	mockToolChat(
		[
			jobList(filterCall),
			jobList({ ...filterCall, result: '{"total": 0}', done: true }),
			"Here is the answer",
		],
		holdReload(),
	);
	await sendChat();

	await expect.element(page.getByText("Here is the answer")).toBeVisible();
	await expect.poll(() => count(page.getByTestId("chat-activity"))).toBe(0);
});

test("user bubbles stay docked right with their text left-aligned", async () => {
	mockMCP({
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
	await renderApp("/agents/assistant/chat");

	const text = page
		.getByTestId("chat-user-message")
		.getByTestId("chat-message-content")
		.filter({ hasText: "short" });
	await expect.element(text).toBeVisible();
	expect(["left", "start"]).toContain(
		getComputedStyle(text.element()).textAlign,
	);

	// Each line starts at the bubble's left padding edge.
	const bubble = text.element().parentElement as HTMLElement;
	const bubbleBox = bubble.getBoundingClientRect();
	const textBox = rect(text);
	const padding = Number.parseFloat(getComputedStyle(bubble).paddingLeft);
	expect(textBox.x).toBeCloseTo(bubbleBox.x + padding, 0);

	// The bubble itself still sits on the right side of the chat.
	const rowBox = (bubble.parentElement as HTMLElement).getBoundingClientRect();
	expect(bubbleBox.x + bubbleBox.width).toBeCloseTo(rowBox.x + rowBox.width, 0);
});

// "links in chat messages open in a new tab" stays in web/e2e/chat.spec.ts:
// it follows the link into a real popup page.

test("channel file attachments show their placeholder, not a broken link", async () => {
	const png =
		"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";
	mockMCP({
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
	await renderApp("/agents/assistant/chat");

	const message = page.getByTestId("chat-user-message");
	await expect
		.element(message)
		.toMatchTextContent(
			"[file attached: chrome-net-export-log.json (application/json, 1.2 MB)",
		);
	await expect.poll(() => count(message.locator("img"))).toBe(1);
	await expect.poll(() => count(message.getByText("Open attachment"))).toBe(0);
	await expect.poll(() => count(message.locator('a[href^="file:"]'))).toBe(0);
});
