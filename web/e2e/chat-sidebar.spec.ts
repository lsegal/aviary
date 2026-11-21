import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

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

/** Mocks one agent whose chats live in `sessions`, recording tool calls. */
async function mockChats(page: Page, sessions: Sess[]) {
	const calls: Array<{ tool: string; args: Record<string, unknown> }> = [];
	const record = (tool: string) => (args?: Record<string, unknown>) => {
		calls.push({ tool, args: args ?? {} });
	};
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: { agents: [{ name: "assistant", model: "x/y" }] },
		config_validate: [],
		job_list: [],
		session_list: () => sessions,
		session_messages: [],
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

function chatRows(page: Page) {
	return page.getByTestId("sidebar-chats").getByTestId("chat-row");
}

test("the Chats list shows Main Chat first, titles, and last-message time on hover", async ({
	page,
}) => {
	await mockChats(page, [
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
			created_at: hoursAgo(1),
		},
	]);
	await page.goto("/agents/assistant/chat");

	const sidebar = page.getByTestId("sidebar-sole-agent");
	await expect(
		sidebar.getByRole("link", { name: "Chats", exact: true }),
	).toBeVisible();
	await expect(chatRows(page)).toHaveText([
		/Main Chat/,
		/Groceries/,
		/New chat/,
	]);
	// "+ New chat" comes after every chat row.
	await expect(
		page
			.getByTestId("sidebar-chats")
			.locator(
				':has([data-testid="chat-row"]) + [data-testid="sidebar-new-chat"]',
			),
	).toBeVisible();
	// The top tabs are gone on wide screens.
	await expect(page.getByTestId("chat-picker")).toBeHidden();

	const groceries = chatRows(page).nth(1);
	const time = groceries.getByTestId("chat-last-message");
	await expect(time).toBeHidden();
	await groceries.hover();
	await expect(time).toBeVisible();
	await expect(time).toHaveText("3h");

	await groceries.getByRole("link").click();
	await expect(page).toHaveURL("/agents/assistant/chat/sess_1");
});

test("double-clicking a chat renames it", async ({ page }) => {
	const calls = await mockChats(page, [
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
	await page.goto("/agents/assistant/chat");

	await chatRows(page).nth(1).getByRole("link").dblclick();
	const input = page.getByTestId("chat-rename-input");
	await expect(input).toBeFocused();
	await expect(input).toHaveValue("Groceries");
	await input.fill("Weekly shop");
	await input.press("Enter");

	await expect(chatRows(page).nth(1)).toHaveText(/Weekly shop/);
	expect(calls).toContainEqual({
		tool: "session_rename",
		args: { agent: "assistant", session_id: "sess_1", name: "Weekly shop" },
	});

	// Escape cancels without saving.
	await chatRows(page).nth(1).getByRole("link").dblclick();
	await page.getByTestId("chat-rename-input").fill("Nope");
	await page.getByTestId("chat-rename-input").press("Escape");
	await expect(chatRows(page).nth(1)).toHaveText(/Weekly shop/);
	expect(calls.filter((c) => c.tool === "session_rename")).toHaveLength(1);
});

test("the … menu renames and deletes a chat after confirmation", async ({
	page,
}) => {
	const calls = await mockChats(page, [
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
	await page.goto("/agents/assistant/chat/sess_1");

	// Main Chat can be renamed but not deleted.
	await chatRows(page).first().hover();
	await chatRows(page).first().getByTestId("chat-menu-button").click();
	await expect(page.getByRole("menuitem", { name: "Rename" })).toBeVisible();
	await expect(page.getByRole("menuitem", { name: "Delete" })).toHaveCount(0);
	await page.keyboard.press("Escape");

	const row = chatRows(page).nth(1);
	await row.hover();
	await row.getByTestId("chat-menu-button").click();
	await page.getByRole("menuitem", { name: "Rename" }).click();
	await expect(page.getByTestId("chat-rename-input")).toBeFocused();
	await page.getByTestId("chat-rename-input").fill("Pantry");
	await page.getByTestId("chat-rename-input").press("Enter");
	await expect(row).toHaveText(/Pantry/);

	await row.hover();
	await row.getByTestId("chat-menu-button").click();
	await page.getByRole("menuitem", { name: "Delete" }).click();
	const dialog = page.getByRole("alertdialog");
	await expect(dialog).toContainText("Pantry");
	await dialog.getByRole("button", { name: "Delete" }).click();

	await expect(chatRows(page)).toHaveText([/Main Chat/]);
	await expect(page).toHaveURL("/agents/assistant/chat/main");
	expect(calls).toContainEqual({
		tool: "session_remove",
		args: { agent: "assistant", session_id: "sess_1" },
	});
});

test("a new chat takes its generated name after the first message", async ({
	page,
}) => {
	await mockChats(page, [
		{
			id: "main",
			agent_id: "assistant",
			name: "main",
			created_at: hoursAgo(2),
		},
	]);
	await page.goto("/agents/assistant/chat");

	await page.getByTestId("sidebar-new-chat").click();
	await expect(page).toHaveURL("/agents/assistant/chat/sess_new");
	await expect(chatRows(page)).toHaveText([/Main Chat/, /New chat/]);

	const input = page.getByPlaceholder("Type a message or paste an image…");
	await input.fill("help me plan a weekend trip");
	await input.press("Enter");

	await expect(chatRows(page)).toHaveText([/Main Chat/, /Weekend trip plans/]);
});
