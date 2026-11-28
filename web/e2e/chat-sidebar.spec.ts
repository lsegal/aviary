// The rest of these scenarios are component tests in
// web/src/scenarios/chat-sidebar.component.test.ts (#1636). This one stays a
// Playwright spec: it needs a touch browser context where (hover: none)
// matches, which page.emulateMedia cannot emulate in a component test.
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
			if (s && !s.title) s.title = "Weekend trip plans";
			return "Sure!";
		},
	});
	return calls;
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
