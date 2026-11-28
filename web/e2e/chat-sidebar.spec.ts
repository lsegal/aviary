// The rest of these scenarios are component tests in
// web/src/scenarios/chat-sidebar.component.test.ts (#1636). These two stay
// Playwright specs: one needs a touch browser context where (hover: none)
// matches, which page.emulateMedia cannot emulate, and the other needs two
// live tabs at once, where a component test mounts one app.
import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";
import { chooseOption } from "./helpers/select";

interface Sess {
	id: string;
	agent_id: string;
	name: string;
	title?: string;
	created_at: string;
	updated_at?: string;
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
async function mockChats(
	page: Page,
	sessions: Sess[],
	messages: Record<string, Msg[]> = {},
) {
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
			if (args?.session_id && !s)
				throw new Error(
					`session "${args.session_id}" not found for agent "assistant"`,
				);
			if (s && !s.title) s.title = "Weekend trip plans";
			return "Sure!";
		},
	});
	return calls;
}

function chatRows(page: Page) {
	return page.getByTestId("sidebar-chats").getByTestId("chat-row");
}

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

/** Opacity of the "+" row action, which hover-capable pointers fade in. */
function opacity(page: Page, testid: string) {
	return page
		.getByTestId(testid)
		.evaluate((el) => Number(getComputedStyle(el).opacity));
}

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
	for (const testid of ["sidebar-new-chat", "sidebar-add-project"]) {
		await expect.poll(() => opacity(page, testid)).toBe(1);
	}

	await page.getByTestId("sidebar-new-chat").tap();
	await expect(page).toHaveURL("/agents/assistant/chat/sess_new");
	await context.close();
});

test("a chat removed on System → Sessions leaves other tabs, and a send to a gone chat stops thinking", async ({
	page,
	context,
}) => {
	const sessions: Sess[] = [
		...twoChats(),
		{
			id: "sess_2",
			agent_id: "assistant",
			name: "sess_2",
			title: "Packing list",
			created_at: hoursAgo(4),
			last_message_at: hoursAgo(2),
		},
	];
	const sockets: Array<{ send: (m: string) => void }> = [];
	await context.routeWebSocket(/\/api\/ws/, (ws) => {
		sockets.push(ws);
	});
	const sessionsTab = await context.newPage();
	await mockChats(page, sessions);
	await mockChats(sessionsTab, sessions);

	await page.goto("/agents/assistant/chat/sess_1");
	await expect(chatRows(page)).toHaveText([
		/Main Chat/,
		/Groceries/,
		/Packing list/,
	]);
	await expect.poll(() => sockets.length).toBeGreaterThan(0);

	await sessionsTab.goto("/system/sessions");
	await chooseOption(
		sessionsTab.getByRole("combobox", { name: "Agent" }),
		"assistant",
	);
	await sessionsTab
		.getByRole("row")
		.filter({ hasText: "Groceries" })
		.getByTestId("session-remove-button")
		.click();
	await sessionsTab
		.getByRole("alertdialog")
		.getByRole("button", { name: "Remove" })
		.click();
	await expect(sessionsTab.getByRole("row", { name: /Groceries/ })).toHaveCount(
		0,
	);
	// The server tells every client about the removal.
	for (const ws of sockets)
		ws.send(
			JSON.stringify({
				type: "session_removed",
				agent_id: "assistant",
				session_id: "sess_1",
			}),
		);

	await expect(page).toHaveURL("/agents/assistant/chat/main");
	await expect(chatRows(page)).toHaveText([/Main Chat/, /Packing list/]);

	// A chat removed without this tab hearing of it fails the send, and the
	// failure ends the turn instead of thinking forever.
	await chatRows(page)
		.filter({ hasText: "Packing list" })
		.getByRole("link")
		.click();
	await expect(page).toHaveURL("/agents/assistant/chat/sess_2");
	sessions.splice(
		sessions.findIndex((s) => s.id === "sess_2"),
		1,
	);
	const input = page.getByPlaceholder("Describe a task or ask a question");
	await input.fill("message into deleted chat");
	await input.press("Enter");
	await expect(page.getByTestId("chat-message-error")).toContainText(
		'session "sess_2" not found for agent "assistant"',
	);
	await expect(page.getByTestId("chat-typing")).toHaveCount(0);
	await expect(page.getByRole("button", { name: "Stop response" })).toHaveCount(
		0,
	);
});
