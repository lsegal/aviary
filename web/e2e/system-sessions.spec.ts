import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken, type ToolFixtures } from "./helpers/mockMCP";
import { chooseValue, expectValue } from "./helpers/select";
import { systemList } from "./helpers/sidebar";

// System → Sessions (/system/sessions, SettingsSessionsTab.vue) (#1673).

interface Session {
	id: string;
	agent_id: string;
	name: string;
	title?: string;
	deletable: boolean;
	is_processing: boolean;
	updated_at: string;
}

const now = new Date().toISOString();

function session(
	agent: string,
	id: string,
	extra: Partial<Session> = {},
): Session {
	return {
		id,
		agent_id: agent,
		name: id,
		deletable: true,
		is_processing: false,
		updated_at: now,
		...extra,
	};
}

function mainChat(agent: string): Session {
	return session(agent, `${agent}_main`, { name: "main", deletable: false });
}

interface Calls {
	list: string[];
	create: string[];
	remove: Array<{ agent?: unknown; session_id?: unknown }>;
	clear: Array<{ agent?: unknown; session_id?: unknown }>;
	stop: Array<{ agent?: unknown; session_id?: unknown }>;
}

/**
 * Mocks a server with two agents and in-memory sessions per agent. The
 * returned `calls` records each session tool call; `overrides` replace the
 * default handlers.
 */
async function mockSessions(
	page: Page,
	store: Record<string, Session[]>,
	overrides: ToolFixtures = {},
	holdCreate: (calls: Calls) => Promise<void> = async () => {},
) {
	const calls: Calls = {
		list: [],
		create: [],
		remove: [],
		clear: [],
		stop: [],
	};
	let created = 0;
	await setAuthToken(page);
	// Keep the real server's live frames out; specs that need them route
	// /api/ws again.
	await page.routeWebSocket(/\/api\/ws/, () => {});
	await mockMCP(page, {
		config_get: {
			server: { port: 16677 },
			agents: [
				{ name: "assistant", model: "anthropic/claude-sonnet-4-5" },
				{ name: "helper", model: "openai/gpt-5" },
			],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
		},
		agent_list: [
			{ id: "assistant", name: "assistant", model: "", state: "idle" },
			{ id: "helper", name: "helper", model: "", state: "idle" },
		],
		session_list: (args) => {
			const agent = String(args?.agent ?? "");
			calls.list.push(agent);
			return store[agent] ?? [];
		},
		session_create: async (args?: Record<string, unknown>) => {
			const agent = String(args?.agent ?? "");
			calls.create.push(agent);
			await holdCreate(calls);
			created += 1;
			const s = session(agent, `sess_new_${created}`);
			store[agent] = [...(store[agent] ?? []), s];
			return s;
		},
		session_remove: (args?: Record<string, unknown>) => {
			calls.remove.push({ ...args });
			const agent = String(args?.agent ?? "");
			store[agent] = (store[agent] ?? []).filter(
				(s) => s.id !== args?.session_id,
			);
			return "removed";
		},
		session_clear: (args?: Record<string, unknown>) => {
			calls.clear.push({ ...args });
			return "cleared";
		},
		session_stop: (args?: Record<string, unknown>) => {
			calls.stop.push({ ...args });
			for (const s of store[String(args?.agent ?? "")] ?? [])
				if (s.id === args?.session_id) s.is_processing = false;
			return "stopped";
		},
		...overrides,
	});
	return calls;
}

function sessionsCard(page: Page) {
	return page
		.locator("section")
		.filter({ has: page.getByRole("heading", { name: "Sessions" }) });
}

function agentPicker(page: Page) {
	return sessionsCard(page).getByLabel("Agent");
}

function rows(page: Page) {
	return sessionsCard(page).locator("tbody tr");
}

function row(page: Page, name: string) {
	return rows(page).filter({
		has: page.getByRole("cell", { name, exact: true }),
	});
}

test("selects the first agent and reloads the table when switching agents", async ({
	page,
}) => {
	const calls = await mockSessions(page, {
		assistant: [
			mainChat("assistant"),
			session("assistant", "a1", { title: "Alpha" }),
		],
		helper: [session("helper", "h1", { title: "Helper chat" })],
	});
	await page.goto("/system/sessions");

	await expectValue(agentPicker(page), "assistant");
	await expect(rows(page).locator("td:first-child")).toHaveText([
		"Main Chat",
		"Alpha",
	]);
	expect(calls.list).toContain("assistant");

	await chooseValue(agentPicker(page), "helper");
	await expect(rows(page).locator("td:first-child")).toHaveText([
		"Helper chat",
	]);
	expect(calls.list).toContain("helper");
});

test("Create Session adds a New chat row, once per click", async ({ page }) => {
	// Once armed, each create waits for the next one, so the second of two
	// quick clicks lands while the first create is still running.
	let pair: { arrived: number; both: Promise<void>; go: () => void } | null =
		null;
	const calls = await mockSessions(
		page,
		{ assistant: [mainChat("assistant")], helper: [] },
		{},
		async () => {
			if (!pair) return;
			pair.arrived += 1;
			if (pair.arrived === 2) pair.go();
			await pair.both;
		},
	);
	await page.goto("/system/sessions");
	await expect(rows(page)).toHaveCount(1);

	const create = sessionsCard(page).getByRole("button", {
		name: "+ Create Session",
	});
	await create.click();
	await expect(rows(page)).toHaveCount(2);
	await expect(row(page, "New chat")).toHaveCount(1);
	expect(calls.create).toEqual(["assistant"]);

	let go: () => void = () => {};
	const both = new Promise<void>((r) => {
		go = r;
	});
	pair = { arrived: 0, both, go };
	await create.click();
	await expect.poll(() => calls.create).toHaveLength(2);
	await expect(create).toBeEnabled();
	await create.click();
	await expect.poll(() => calls.create).toHaveLength(3);
	await expect(rows(page)).toHaveCount(4);
	await expect(row(page, "New chat")).toHaveCount(3);
	expect(calls.create).toEqual(["assistant", "assistant", "assistant"]);
});

test("Main Chat is cleared and other chats are removed, and Cancel changes nothing", async ({
	page,
}) => {
	const calls = await mockSessions(page, {
		assistant: [
			mainChat("assistant"),
			session("assistant", "a1", { title: "Alpha" }),
		],
		helper: [],
	});
	await page.goto("/system/sessions");
	const dialog = page.getByRole("alertdialog");

	// Main Chat is not deletable, so it offers Clear.
	const main = row(page, "Main Chat");
	await main.getByRole("button", { name: "Clear" }).click();
	await expect(dialog).toContainText("Clear Main Chat?");
	await expect(dialog).toContainText("The chat itself stays.");
	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect(dialog).toHaveCount(0);
	expect(calls.clear).toEqual([]);

	await main.getByRole("button", { name: "Clear" }).click();
	await dialog.getByRole("button", { name: "Clear", exact: true }).click();
	await expect(dialog).toHaveCount(0);
	await expect
		.poll(() => calls.clear)
		.toEqual([{ agent: "assistant", session_id: "assistant_main" }]);
	await expect(row(page, "Main Chat")).toHaveCount(1);

	// Other chats are deletable, so they offer Remove.
	const alpha = row(page, "Alpha");
	await alpha.getByRole("button", { name: "Remove" }).click();
	await expect(dialog).toContainText("Remove session?");
	await expect(dialog).toContainText("permanently delete");
	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect(dialog).toHaveCount(0);
	expect(calls.remove).toEqual([]);
	await expect(rows(page)).toHaveCount(2);

	await alpha.getByRole("button", { name: "Remove" }).click();
	await dialog.getByRole("button", { name: "Remove", exact: true }).click();
	await expect(dialog).toHaveCount(0);
	expect(calls.remove).toEqual([{ agent: "assistant", session_id: "a1" }]);
	await expect(rows(page).locator("td:first-child")).toHaveText(["Main Chat"]);
	expect(calls.clear).toHaveLength(1);
});

test("Stop is enabled live by session_processing and disabled again once stopped", async ({
	page,
}) => {
	const store = {
		assistant: [session("assistant", "a1", { title: "Busy" })],
		helper: [],
	};
	const calls = await mockSessions(page, store);
	const sockets: Array<{ send: (m: string) => void }> = [];
	await page.routeWebSocket(/\/api\/ws/, (ws) => {
		sockets.push(ws);
	});
	await page.goto("/system/sessions");

	const stop = row(page, "Busy").getByRole("button", { name: "Stop" });
	await expect(stop).toBeDisabled();
	await expect.poll(() => sockets.length).toBeGreaterThan(0);

	store.assistant[0].is_processing = true;
	for (const ws of sockets)
		ws.send(
			JSON.stringify({
				type: "session_processing",
				agent_id: "assistant",
				session_id: "a1",
			}),
		);
	await expect(stop).toBeEnabled();

	await stop.click();
	await expect(stop).toBeDisabled();
	expect(calls.stop).toEqual([{ agent: "assistant", session_id: "a1" }]);
});

test("leaving for Skills and coming back keeps the agent and reloads its sessions", async ({
	page,
}) => {
	const store: Record<string, Session[]> = {
		assistant: [session("assistant", "a1", { title: "Alpha" })],
		helper: [session("helper", "h1", { title: "Helper chat" })],
	};
	const calls = await mockSessions(page, store);
	await page.goto("/system/sessions");
	await chooseValue(agentPicker(page), "helper");
	await expect(rows(page).locator("td:first-child")).toHaveText([
		"Helper chat",
	]);

	await (await systemList(page))
		.getByRole("link", { name: "Skills", exact: true })
		.click();
	await expect(page).toHaveURL(/\/system\/skills$/);

	store.helper.push(session("helper", "h2", { title: "Arrived meanwhile" }));
	const before = calls.list.filter((a) => a === "helper").length;
	await (await systemList(page))
		.getByRole("link", { name: "Sessions", exact: true })
		.click();
	await expect(page).toHaveURL(/\/system\/sessions$/);

	await expectValue(agentPicker(page), "helper");
	await expect(rows(page).locator("td:first-child")).toHaveText([
		"Helper chat",
		"Arrived meanwhile",
	]);
	expect(calls.list.filter((a) => a === "helper").length).toBeGreaterThan(
		before,
	);
});

test("an empty list says so", async ({ page }) => {
	await mockSessions(page, { assistant: [], helper: [] });
	await page.goto("/system/sessions");
	await expect(
		sessionsCard(page).getByText("No sessions found."),
	).toBeVisible();
	await expect(sessionsCard(page).locator("table")).toHaveCount(0);
});

test("a failing session_list shows the error and an empty table", async ({
	page,
}) => {
	await mockSessions(
		page,
		{},
		{
			session_list: () => {
				throw new Error("session store unavailable");
			},
		},
	);
	await page.goto("/system/sessions");
	await expect(page.getByText("session store unavailable")).toBeVisible();
	await expect(
		sessionsCard(page).getByText("No sessions found."),
	).toBeVisible();
	await expect(sessionsCard(page).locator("table")).toHaveCount(0);
});

test("a slow list for the previous agent does not replace the current agent's sessions", async ({
	page,
}) => {
	const store: Record<string, Session[]> = {
		assistant: [session("assistant", "a1", { title: "Alpha" })],
		helper: [session("helper", "h1", { title: "Helper chat" })],
	};
	let slow = false;
	let release: () => void = () => {};
	const held = new Promise<void>((r) => {
		release = r;
	});
	await mockSessions(page, store, {
		session_list: async (args) => {
			const agent = String(args?.agent ?? "");
			if (agent === "assistant" && slow) await held;
			return store[agent] ?? [];
		},
	});
	await page.goto("/system/sessions");
	await expect(rows(page).locator("td:first-child")).toHaveText(["Alpha"]);

	// Hold assistant's next list, refresh it, then switch to helper.
	slow = true;
	await sessionsCard(page)
		.getByRole("button", { name: "Refresh Sessions" })
		.click();
	await chooseValue(agentPicker(page), "helper");
	await expect(rows(page).locator("td:first-child")).toHaveText([
		"Helper chat",
	]);

	release();
	// Give the stale response time to arrive and be ignored.
	await page.waitForTimeout(300);
	await expect(rows(page).locator("td:first-child")).toHaveText([
		"Helper chat",
	]);
	await expectValue(agentPicker(page), "helper");
});
