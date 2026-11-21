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
			created_at: hoursAgo(4),
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
	// New chat is the "+" on the Chats row, not a row in the list (#638).
	await expect(
		page.getByTestId("sidebar-chats").getByTestId("sidebar-new-chat"),
	).toHaveCount(0);
	await expect(page.getByTestId("sidebar-new-chat")).toHaveCount(1);
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

	const input = page.getByPlaceholder("Describe a task or ask a question");
	await input.fill("help me plan a weekend trip");
	await input.press("Enter");

	await expect(chatRows(page)).toHaveText([/Main Chat/, /Weekend trip plans/]);
});

/** Opacity of the "+" row action, which hover-capable pointers fade in. */
function opacity(page: Page, testid: string) {
	return page
		.getByTestId(testid)
		.evaluate((el) => Number(getComputedStyle(el).opacity));
}

test('the Chats "+" shows on hover or keyboard focus and creates a new chat', async ({
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

	const plus = page.getByTestId("sidebar-new-chat");
	await expect(plus).toHaveAttribute("aria-label", "New chat");
	await expect(plus).toHaveAttribute("title", "New chat");
	await expect.poll(() => opacity(page, "sidebar-new-chat")).toBe(0);

	const chats = page
		.getByTestId("sidebar-sole-agent")
		.getByRole("link", { name: "Chats", exact: true });
	await chats.hover();
	await expect.poll(() => opacity(page, "sidebar-new-chat")).toBe(1);

	await page.mouse.move(800, 600);
	await expect.poll(() => opacity(page, "sidebar-new-chat")).toBe(0);
	await chats.focus();
	await expect.poll(() => opacity(page, "sidebar-new-chat")).toBe(1);
	await page.keyboard.press("Tab");
	await expect(plus).toBeFocused();
	await expect.poll(() => opacity(page, "sidebar-new-chat")).toBe(1);

	await page.keyboard.press("Enter");
	await expect(page).toHaveURL("/agents/assistant/chat/sess_new");
	await expect(chatRows(page)).toHaveText([/Main Chat/, /New chat/]);
});

test('on a touch screen the sidebar\'s "+" actions show without hovering', async ({
	browser,
}) => {
	const context = await browser.newContext({
		viewport: { width: 1280, height: 800 },
		hasTouch: true,
		isMobile: true,
	});
	const page = await context.newPage();
	await mockChats(page, [
		{
			id: "main",
			agent_id: "assistant",
			name: "main",
			created_at: hoursAgo(2),
		},
	]);
	await page.goto("/agents/assistant/chat");
	expect(await page.evaluate(() => matchMedia("(hover: none)").matches)).toBe(
		true,
	);

	await expect(page.getByTestId("sidebar-sole-agent")).toBeVisible();
	for (const testid of [
		"sidebar-new-chat",
		"sidebar-add-project",
		"sidebar-add-agent",
	]) {
		await expect.poll(() => opacity(page, testid)).toBe(1);
	}

	await page.getByTestId("sidebar-new-chat").tap();
	await expect(page).toHaveURL("/agents/assistant/chat/sess_new");
	await context.close();
});

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

test("the Chats list is expanded on load away from the chat page", async ({
	page,
}) => {
	await mockChats(page, manyChats(2));
	await page.goto("/system/overview");

	await expect(chatRows(page)).toHaveText([/Main Chat/, /Chat 1/, /Chat 2/]);
	await expect(page.getByTestId("sidebar-new-chat")).toBeVisible();
	// No chat is selected away from the chat pages.
	await expect(chatRows(page).first().getByRole("link")).not.toHaveClass(
		/font-semibold/,
	);
});

test("five or fewer chats are all shown without More", async ({ page }) => {
	await mockChats(page, manyChats(4));
	await page.goto("/agents/assistant/chat");

	await expect(chatRows(page)).toHaveText([
		/Main Chat/,
		/Chat 1/,
		/Chat 2/,
		/Chat 3/,
		/Chat 4/,
	]);
	await expect(page.getByTestId("sidebar-chats-more")).toHaveCount(0);
});

test("more than five chats show the five most recent and More reveals the rest", async ({
	page,
}) => {
	await mockChats(page, manyChats(7));
	await page.goto("/system/overview");

	await expect(chatRows(page)).toHaveText([
		/Main Chat/,
		/Chat 1/,
		/Chat 2/,
		/Chat 3/,
		/Chat 4/,
	]);
	const more = page.getByTestId("sidebar-chats-more");
	await expect(more).toHaveText("More");
	// New chat is the "+" on the Chats row, so More ends the list (#638).
	await expect(
		page.getByTestId("sidebar-chats").locator("> button"),
	).toHaveText(["More"]);

	await more.click();
	await expect(chatRows(page)).toHaveCount(8);
	await expect(chatRows(page).nth(7)).toHaveText(/Chat 7/);

	await more.click();
	await expect(chatRows(page)).toHaveCount(5);
});

test("the Chats list follows chats created, renamed and deleted elsewhere", async ({
	page,
}) => {
	const sessions = manyChats(1);
	await mockChats(page, sessions);
	const sockets: Array<{ send: (m: string) => void }> = [];
	await page.routeWebSocket(/\/api\/ws/, (ws) => {
		sockets.push(ws);
	});
	await page.goto("/system/overview");
	await expect(chatRows(page)).toHaveText([/Main Chat/, /Chat 1/]);
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
	await expect(chatRows(page)).toHaveText([/Main Chat/, /Fresh/, /Chat 1/]);

	sessions[1].title = "Renamed";
	sessions.pop();
	notify();
	await expect(chatRows(page)).toHaveText([/Main Chat/, /Renamed/]);
});
