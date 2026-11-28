import { expect, test } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, reload, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import {
	mockMCP,
	routeWebSocket,
	type WebSocketRoute,
} from "../component-test/server";
import { chooseOption } from "./helpers/select";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../views/ChatView.vue"),
	() => import("../views/OverviewView.vue"),
	() => import("../views/SettingsView.vue"),
);

interface Sess {
	id: string;
	agent_id: string;
	name: string;
	title?: string;
	created_at: string;
	last_message_at?: string;
}

const hoursAgo = (h: number) =>
	new Date(Date.now() - h * 3_600_000).toISOString();

interface Msg {
	id: string;
	role: "user" | "assistant";
	content: string;
	timestamp: string;
}

/** The server's rule: Main Chat can only be cleared, never deleted. */
const deletable = (s: Sess) => s.name !== "main";

/**
 * Mocks one agent whose chats live in `sessions`, and their messages in
 * `messages` by session ID, recording tool calls.
 */
function mockChats(sessions: Sess[], messages: Record<string, Msg[]> = {}) {
	const calls: Array<{ tool: string; args: Record<string, unknown> }> = [];
	const record = (tool: string) => (args?: Record<string, unknown>) => {
		calls.push({ tool, args: args ?? {} });
	};
	mockMCP({
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: { agents: [{ name: "assistant", model: "x/y" }] },
		config_validate: [],
		job_list: [],
		session_list: () =>
			sessions.map((s) => ({ ...s, deletable: deletable(s) })),
		session_messages: (args?: Record<string, unknown>) =>
			messages[String(args?.session_id)] ?? [],
		session_clear: (args?: Record<string, unknown>) => {
			record("session_clear")(args);
			const s = sessions.find((x) => x.id === args?.session_id);
			if (!s) throw new Error(`session ${args?.session_id} not found`);
			delete messages[s.id];
			delete s.last_message_at;
			return "cleared";
		},
		session_create: () => {
			const sess = {
				id: "sess_new",
				agent_id: "assistant",
				name: "sess_new",
				created_at: new Date().toISOString(),
			};
			sessions.push(sess);
			return sess;
		},
		session_rename: (args?: Record<string, unknown>) => {
			record("session_rename")(args);
			const s = sessions.find((x) => x.id === args?.session_id);
			if (s) s.title = String(args?.name);
			return "renamed";
		},
		session_remove: (args?: Record<string, unknown>) => {
			record("session_remove")(args);
			const i = sessions.findIndex((x) => x.id === args?.session_id);
			if (i >= 0 && !deletable(sessions[i]))
				throw new Error(
					`session "${args?.session_id}" cannot be deleted; clear it instead`,
				);
			if (i >= 0) sessions.splice(i, 1);
			return "removed";
		},
		agent_run: (args?: Record<string, unknown>) => {
			record("agent_run")(args);
			// The server titles a new chat from its first prompt.
			const s = sessions.find((x) => x.id === args?.session_id);
			if (s && !s.title) s.title = "Weekend trip plans";
			return "Sure!";
		},
	});
	return calls;
}

function chatRows() {
	return page.getByTestId("sidebar-chats").getByTestId("chat-row");
}

function count(loc: Locator): number {
	return loc.elements().length;
}

/** Each matched element's text, whitespace collapsed. */
function texts(loc: Locator): string[] {
	return loc
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
}

/** As Playwright's toHaveText with an array: as many elements as
 * `expected`, each equal to its string or matching its RegExp. */
async function expectTexts(loc: Locator, expected: (string | RegExp)[]) {
	await expect
		.poll(() => {
			const actual = texts(loc);
			return (
				actual.length === expected.length &&
				expected.every((e, i) =>
					typeof e === "string" ? actual[i] === e : e.test(actual[i]),
				)
			);
		})
		.toBe(true);
}

/** As Playwright's toBeHidden: absent, or present without a box. */
async function expectHidden(loc: Locator) {
	await expect
		.poll(() => {
			const el = loc.query();
			return !el || el.getClientRects().length === 0;
		})
		.toBe(true);
}

/** Focuses `loc` and presses `key`, as Playwright's locator.press. */
async function press(loc: Locator, key: string) {
	(loc.element() as HTMLElement).focus();
	await commands.keyPress(key);
}

test("the Chats list shows Main Chat first, titles, and last-message time on hover", async () => {
	mockChats([
		{
			id: "main",
			agent_id: "assistant",
			name: "main",
			created_at: hoursAgo(48),
			last_message_at: hoursAgo(11),
		},
		{
			id: "sess_1",
			agent_id: "assistant",
			name: "sess_1",
			title: "Groceries",
			created_at: hoursAgo(5),
			last_message_at: hoursAgo(3),
		},
		{
			id: "sess_2",
			agent_id: "assistant",
			name: "sess_2",
			created_at: hoursAgo(4),
		},
	]);
	await renderApp("/agents/assistant/chat");

	const sidebar = page.getByTestId("sidebar-sole-agent");
	await expect
		.element(sidebar.getByRole("link", { name: "Chats", exact: true }))
		.toBeVisible();
	await expectTexts(chatRows(), [/Main Chat/, /Groceries/, /New chat/]);
	// New chat is the "+" on the Chats row, not a row in the list (#638).
	expect(
		count(page.getByTestId("sidebar-chats").getByTestId("sidebar-new-chat")),
	).toBe(0);
	expect(count(page.getByTestId("sidebar-new-chat"))).toBe(1);
	// The top tabs are gone on wide screens.
	await expectHidden(page.getByTestId("chat-picker"));

	const groceries = chatRows().nth(1);
	const time = groceries.getByTestId("chat-last-message");
	await expectHidden(time);
	await groceries.hover();
	await expect.element(time).toBeVisible();
	await expect.element(time).toHaveText("3h");

	// The time is the last message's, never the chat's creation time (#661).
	const main = chatRows().nth(0);
	await main.hover();
	await expect.element(main.getByTestId("chat-last-message")).toHaveText("11h");
	const empty = chatRows().nth(2);
	await empty.hover();
	expect(count(empty.getByTestId("chat-last-message"))).toBe(0);

	await groceries.getByRole("link").click();
	await expect.poll(currentURL).toBe("/agents/assistant/chat/sess_1");
});

test("double-clicking a chat renames it", async () => {
	const calls = mockChats([
		{
			id: "main",
			agent_id: "assistant",
			name: "main",
			created_at: hoursAgo(2),
		},
		{
			id: "sess_1",
			agent_id: "assistant",
			name: "sess_1",
			title: "Groceries",
			created_at: hoursAgo(1),
		},
	]);
	await renderApp("/agents/assistant/chat");

	await chatRows().nth(1).getByRole("link").dblClick();
	const input = page.getByTestId("chat-rename-input");
	await expect.element(input).toHaveFocus();
	await expect.element(input).toHaveValue("Groceries");
	await input.fill("Weekly shop");
	await press(input, "Enter");

	await expect.element(chatRows().nth(1)).toHaveText(/Weekly shop/);
	expect(calls).toContainEqual({
		tool: "session_rename",
		args: { agent: "assistant", session_id: "sess_1", name: "Weekly shop" },
	});

	// Escape cancels without saving.
	await chatRows().nth(1).getByRole("link").dblClick();
	await page.getByTestId("chat-rename-input").fill("Nope");
	await press(page.getByTestId("chat-rename-input"), "Escape");
	await expect.element(chatRows().nth(1)).toHaveText(/Weekly shop/);
	expect(calls.filter((c) => c.tool === "session_rename")).toHaveLength(1);
});

test("the … menu renames and deletes a chat after confirmation", async () => {
	const calls = mockChats([
		{
			id: "main",
			agent_id: "assistant",
			name: "main",
			created_at: hoursAgo(2),
		},
		{
			id: "sess_1",
			agent_id: "assistant",
			name: "sess_1",
			title: "Groceries",
			created_at: hoursAgo(1),
		},
	]);
	await renderApp("/agents/assistant/chat/sess_1");

	// Main Chat can be renamed and cleared but not deleted.
	await chatRows().first().hover();
	await chatRows().first().getByTestId("chat-menu-button").click();
	await expectTexts(page.getByRole("menuitem"), ["Rename", "Clear"]);
	await commands.keyPress("Escape");

	const row = chatRows().nth(1);
	await row.hover();
	await row.getByTestId("chat-menu-button").click();
	await expectTexts(page.getByRole("menuitem"), ["Rename", "Delete"]);
	await page.getByRole("menuitem", { name: "Rename" }).click();
	await expect.element(page.getByTestId("chat-rename-input")).toHaveFocus();
	await page.getByTestId("chat-rename-input").fill("Pantry");
	await press(page.getByTestId("chat-rename-input"), "Enter");
	await expect.element(row).toHaveText(/Pantry/);

	await row.hover();
	await row.getByTestId("chat-menu-button").click();
	await page.getByRole("menuitem", { name: "Delete" }).click();
	const dialog = page.getByRole("alertdialog");
	await expect.element(dialog).toMatchTextContent("Pantry");
	await dialog.getByRole("button", { name: "Delete" }).click();

	await expectTexts(chatRows(), [/Main Chat/]);
	await expect.poll(currentURL).toBe("/agents/assistant/chat/main");
	expect(calls).toContainEqual({
		tool: "session_remove",
		args: { agent: "assistant", session_id: "sess_1" },
	});
});

const mainHistory = (): Msg[] => [
	{
		id: "m1",
		role: "user",
		content: "remember the milk",
		timestamp: hoursAgo(3),
	},
	{
		id: "m2",
		role: "assistant",
		content: "Noted: milk",
		timestamp: hoursAgo(3),
	},
];

function twoChats(): Sess[] {
	return [
		{
			id: "main",
			agent_id: "assistant",
			name: "main",
			created_at: hoursAgo(48),
			last_message_at: hoursAgo(3),
		},
		{
			id: "sess_1",
			agent_id: "assistant",
			name: "sess_1",
			title: "Groceries",
			created_at: hoursAgo(5),
			last_message_at: hoursAgo(1),
		},
	];
}

test("clearing Main Chat after confirmation empties it and keeps it in the list", async () => {
	const calls = mockChats(twoChats(), { main: mainHistory() });
	await renderApp("/agents/assistant/chat");
	await expect.element(page.getByText("Noted: milk")).toBeVisible();
	const composer = page.getByPlaceholder("Describe a task or ask a question");
	await composer.fill("half-typed thought");

	const row = chatRows().first();
	await row.hover();
	await row.getByTestId("chat-menu-button").click();
	await page.getByRole("menuitem", { name: "Clear" }).click();
	const dialog = page.getByRole("alertdialog");
	await expect
		.element(dialog.getByRole("heading"))
		.toHaveText("Clear Main Chat?");
	await expect
		.element(dialog)
		.toMatchTextContent(
			"This removes all messages in this chat. The chat itself stays.",
		);
	await expectTexts(dialog.getByRole("button"), ["Cancel", "Clear"]);
	await expect
		.element(dialog.getByRole("button", { name: "Clear" }))
		.toHaveClass(/bg-red-600/);
	await dialog.getByRole("button", { name: "Clear" }).click();

	await expect.poll(() => count(dialog)).toBe(0);
	await expect.poll(() => count(page.getByText("Noted: milk"))).toBe(0);
	await expect.poll(() => count(page.getByText("remember the milk"))).toBe(0);
	// The draft goes with the messages.
	await expect.element(composer).toHaveValue("");
	await expectTexts(chatRows(), [/Main Chat/, /Groceries/]);
	await expect.poll(currentURL).toBe("/agents/assistant/chat/main");
	expect(calls).toContainEqual({
		tool: "session_clear",
		args: { agent: "assistant", session_id: "main" },
	});
	expect(calls.filter((c) => c.tool === "session_remove")).toHaveLength(0);

	// The draft stays gone after a reload.
	await reload();
	await expectTexts(chatRows(), [/Main Chat/, /Groceries/]);
	await expect.element(composer).toHaveValue("");
});

test("cancelling Clear changes nothing", async () => {
	const calls = mockChats(twoChats(), { main: mainHistory() });
	await renderApp("/agents/assistant/chat");
	await expect.element(page.getByText("Noted: milk")).toBeVisible();

	const row = chatRows().first();
	await row.hover();
	await row.getByTestId("chat-menu-button").click();
	await page.getByRole("menuitem", { name: "Clear" }).click();
	const dialog = page.getByRole("alertdialog");
	await dialog.getByRole("button", { name: "Cancel" }).click();

	await expect.poll(() => count(dialog)).toBe(0);
	await expect.element(page.getByText("Noted: milk")).toBeVisible();
	await expectTexts(chatRows(), [/Main Chat/, /Groceries/]);
	expect(calls.filter((c) => c.tool === "session_clear")).toHaveLength(0);
});

test("chats revealed by More keep their Delete action", async () => {
	mockChats(manyChats(7));
	await renderApp("/system/overview");
	await page.getByTestId("sidebar-chats-more").click();

	const last = chatRows().nth(7);
	await last.hover();
	await last.getByTestId("chat-menu-button").click();
	await expectTexts(page.getByRole("menuitem"), ["Rename", "Delete"]);
	await commands.keyPress("Escape");
});

test("Settings → Sessions offers Clear, not Remove, for Main Chat", async () => {
	const calls = mockChats(twoChats(), { main: mainHistory() });
	await renderApp("/system/sessions");
	await chooseOption(
		page.getByRole("combobox", { name: "Agent" }),
		"assistant",
	);

	const buttons = page.getByTestId("session-remove-button");
	await expectTexts(buttons, ["Clear", "Remove"]);

	await buttons.first().click();
	const dialog = page.getByRole("alertdialog");
	await expect
		.element(dialog.getByRole("heading"))
		.toHaveText("Clear Main Chat?");
	await dialog.getByRole("button", { name: "Clear" }).click();
	await expect.poll(() => count(dialog)).toBe(0);
	await expectTexts(buttons, ["Clear", "Remove"]);
	expect(calls).toContainEqual({
		tool: "session_clear",
		args: { agent: "assistant", session_id: "main" },
	});

	await buttons.nth(1).click();
	await expect
		.element(dialog.getByRole("heading"))
		.toHaveText("Remove session?");
	await dialog.getByRole("button", { name: "Remove" }).click();
	await expectTexts(buttons, ["Clear"]);
	expect(calls).toContainEqual({
		tool: "session_remove",
		args: { agent: "assistant", session_id: "sess_1" },
	});
});

test("a new chat takes its generated name after the first message", async () => {
	mockChats([
		{
			id: "main",
			agent_id: "assistant",
			name: "main",
			created_at: hoursAgo(2),
		},
	]);
	await renderApp("/agents/assistant/chat");

	await page.getByTestId("sidebar-new-chat").click();
	await expect.poll(currentURL).toBe("/agents/assistant/chat/sess_new");
	await expectTexts(chatRows(), [/Main Chat/, /New chat/]);

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await input.fill("help me plan a weekend trip");
	await press(input, "Enter");

	await expectTexts(chatRows(), [/Main Chat/, /Weekend trip plans/]);
});

/** Opacity of the "+" row action, which hover-capable pointers fade in. */
function opacity(testid: string) {
	return Number(getComputedStyle(page.getByTestId(testid).element()).opacity);
}

test('the Chats "+" shows on hover or keyboard focus and creates a new chat', async () => {
	mockChats([
		{
			id: "main",
			agent_id: "assistant",
			name: "main",
			created_at: hoursAgo(2),
		},
	]);
	await renderApp("/agents/assistant/chat");

	const plus = page.getByTestId("sidebar-new-chat");
	await expect.element(plus).toHaveAttribute("aria-label", "New chat");
	await expect.element(plus).toHaveAttribute("title", "New chat");
	await expect.poll(() => opacity("sidebar-new-chat")).toBe(0);

	const chats = page
		.getByTestId("sidebar-sole-agent")
		.getByRole("link", { name: "Chats", exact: true });
	await chats.hover();
	await expect.poll(() => opacity("sidebar-new-chat")).toBe(1);

	await commands.mouseMove(800, 600);
	await expect.poll(() => opacity("sidebar-new-chat")).toBe(0);
	(chats.element() as HTMLElement).focus();
	await expect.poll(() => opacity("sidebar-new-chat")).toBe(1);
	await commands.keyPress("Tab");
	await expect.element(plus).toHaveFocus();
	await expect.poll(() => opacity("sidebar-new-chat")).toBe(1);

	await commands.keyPress("Enter");
	await expect.poll(currentURL).toBe("/agents/assistant/chat/sess_new");
	await expectTexts(chatRows(), [/Main Chat/, /New chat/]);
});

// 'on a touch screen the sidebar's "+" actions show without hovering' stays
// in web/e2e/chat-sidebar.spec.ts: it needs a touch context where
// (hover: none) matches, which page.emulateMedia cannot give.

/** Main Chat plus `count` titled chats, chat N last messaged N hours ago. */
function manyChats(count: number): Sess[] {
	const chats: Sess[] = [
		{
			id: "main",
			agent_id: "assistant",
			name: "main",
			created_at: hoursAgo(100),
			last_message_at: hoursAgo(50),
		},
	];
	// Listed oldest first so the sidebar has to order them itself.
	for (let i = count; i >= 1; i--) {
		chats.push({
			id: `sess_${i}`,
			agent_id: "assistant",
			name: `sess_${i}`,
			title: `Chat ${i}`,
			created_at: hoursAgo(60),
			last_message_at: hoursAgo(i),
		});
	}
	return chats;
}

test("the Chats list is expanded on load away from the chat page", async () => {
	mockChats(manyChats(2));
	await renderApp("/system/overview");

	await expectTexts(chatRows(), [/Main Chat/, /Chat 1/, /Chat 2/]);
	await expect.element(page.getByTestId("sidebar-new-chat")).toBeVisible();
	// No chat is selected away from the chat pages.
	await expect
		.element(chatRows().first().getByRole("link"))
		.not.toHaveClass(/font-semibold/);
});

test("five or fewer chats are all shown without More", async () => {
	mockChats(manyChats(4));
	await renderApp("/agents/assistant/chat");

	await expectTexts(chatRows(), [
		/Main Chat/,
		/Chat 1/,
		/Chat 2/,
		/Chat 3/,
		/Chat 4/,
	]);
	expect(count(page.getByTestId("sidebar-chats-more"))).toBe(0);
});

test("more than five chats show the five most recent and More reveals the rest", async () => {
	mockChats(manyChats(7));
	await renderApp("/system/overview");

	await expectTexts(chatRows(), [
		/Main Chat/,
		/Chat 1/,
		/Chat 2/,
		/Chat 3/,
		/Chat 4/,
	]);
	const more = page.getByTestId("sidebar-chats-more");
	await expect.element(more).toHaveText("More");
	// New chat is the "+" on the Chats row, so More ends the list (#638).
	await expectTexts(
		page.getByTestId("sidebar-chats").locator(":scope > button"),
		["More"],
	);

	await more.click();
	await expect.poll(() => count(chatRows())).toBe(8);
	await expect.element(chatRows().nth(7)).toHaveText(/Chat 7/);

	await more.click();
	await expect.poll(() => count(chatRows())).toBe(5);
});

test("the Chats list follows chats created, renamed and deleted elsewhere", async () => {
	const sessions = manyChats(1);
	mockChats(sessions);
	const sockets: WebSocketRoute[] = [];
	routeWebSocket(/\/api\/ws/, (ws) => {
		sockets.push(ws);
	});
	await renderApp("/system/overview");
	await expectTexts(chatRows(), [/Main Chat/, /Chat 1/]);
	await expect.poll(() => sockets.length).toBeGreaterThan(0);

	const notify = () => {
		for (const ws of sockets)
			ws.send(
				JSON.stringify({ type: "session_message", agent_id: "assistant" }),
			);
	};
	sessions.push({
		id: "sess_x",
		agent_id: "assistant",
		name: "sess_x",
		title: "Fresh",
		created_at: hoursAgo(0),
		last_message_at: hoursAgo(0),
	});
	notify();
	await expectTexts(chatRows(), [/Main Chat/, /Fresh/, /Chat 1/]);

	sessions[1].title = "Renamed";
	sessions.pop();
	notify();
	await expectTexts(chatRows(), [/Main Chat/, /Renamed/]);
});
