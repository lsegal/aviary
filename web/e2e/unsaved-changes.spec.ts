/// <reference types="node" />
import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

const PIXEL_PNG = Buffer.from(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
	"base64",
);

/** Counts the window's beforeunload listeners in window.__beforeUnload. The
 * dev server's client registers one of its own, so tests compare against the
 * count on load. */
async function trackBeforeUnload(page: Page) {
	await page.addInitScript(() => {
		const w = window as unknown as { __beforeUnload: number };
		w.__beforeUnload = 0;
		const add = window.addEventListener.bind(window);
		const remove = window.removeEventListener.bind(window);
		window.addEventListener = ((type: string, ...rest: never[]) => {
			if (type === "beforeunload") w.__beforeUnload += 1;
			return (add as (...a: unknown[]) => void)(type, ...rest);
		}) as typeof window.addEventListener;
		window.removeEventListener = ((type: string, ...rest: never[]) => {
			if (type === "beforeunload") w.__beforeUnload -= 1;
			return (remove as (...a: unknown[]) => void)(type, ...rest);
		}) as typeof window.removeEventListener;
	});
}

function beforeUnloadCount(page: Page) {
	return page.evaluate(
		() => (window as unknown as { __beforeUnload: number }).__beforeUnload,
	);
}

/** Mocks one agent with two chats, Main Chat and Research. */
async function mockChats(page: Page) {
	const runs: Array<Record<string, unknown>> = [];
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: { agents: [{ name: "assistant", model: "x/y" }] },
		config_validate: [],
		job_list: [],
		session_list: [
			{
				id: "s1",
				agent_id: "assistant",
				name: "main",
				created_at: "2026-03-12T12:00:00Z",
			},
			{
				id: "s2",
				agent_id: "assistant",
				name: "s2",
				title: "Research",
				created_at: "2026-03-12T13:00:00Z",
			},
		],
		session_messages: [],
		agent_run: (args?: Record<string, unknown>) => {
			runs.push(args ?? {});
			return { stream: ["ok"], result: "" };
		},
	});
	return runs;
}

function messageBox(page: Page) {
	return page.getByTestId("chat-composer").getByRole("textbox");
}

test("a chat's draft is kept across route changes and reloads", async ({
	page,
}) => {
	await trackBeforeUnload(page);
	await mockChats(page);
	await page.goto("/agents/assistant/chat/s1");
	await expect(messageBox(page)).toBeEditable();
	const base = await beforeUnloadCount(page);
	await messageBox(page).fill("half-typed thought");
	const chooser = page.waitForEvent("filechooser");
	await page.getByRole("button", { name: "Attach files" }).click();
	await (await chooser).setFiles([
		{ name: "shot.png", mimeType: "image/png", buffer: PIXEL_PNG },
	]);
	await expect(page.getByTestId("attachment-image")).toHaveCount(1);

	// Composers keep drafts instead of asking before leaving.
	expect(await beforeUnloadCount(page)).toBe(base);
	await page.getByRole("link", { name: "Overview", exact: true }).click();
	await expect(page).toHaveURL(/\/system\/overview$/);
	await expect(page.getByTestId("unsaved-changes-dialog")).toHaveCount(0);

	await page.goBack();
	await expect(messageBox(page)).toHaveValue("half-typed thought");
	await expect(page.getByTestId("attachment-image")).toHaveCount(1);

	await page.reload();
	await expect(messageBox(page)).toHaveValue("half-typed thought");
	await expect(page.getByTestId("attachment-image")).toHaveCount(1);
});

test("each chat keeps its own draft, cleared once sent", async ({ page }) => {
	const runs = await mockChats(page);
	await page.goto("/agents/assistant/chat/s1");
	await messageBox(page).fill("for main");

	const rows = page.getByTestId("sidebar-chats").getByTestId("chat-row");
	await rows.filter({ hasText: "Research" }).click();
	await expect(page).toHaveURL(/\/chat\/s2$/);
	await expect(messageBox(page)).toHaveValue("");
	await messageBox(page).fill("for research");

	await rows.filter({ hasText: "Main Chat" }).click();
	await expect(page).toHaveURL(/\/chat\/s1$/);
	await expect(messageBox(page)).toHaveValue("for main");

	await messageBox(page).press("Enter");
	await expect.poll(() => runs.length).toBe(1);
	await expect(messageBox(page)).toHaveValue("");

	await rows.filter({ hasText: "Research" }).click();
	await expect(messageBox(page)).toHaveValue("for research");
	await rows.filter({ hasText: "Main Chat" }).click();
	await expect(page).toHaveURL(/\/chat\/s1$/);
	await expect(messageBox(page)).toHaveValue("");

	await page.reload();
	await expect(messageBox(page)).toHaveValue("");
});

test("stale stored drafts are dropped on load", async ({ page }) => {
	await page.addInitScript(() => {
		if (sessionStorage.getItem("seeded")) return;
		sessionStorage.setItem("seeded", "1");
		localStorage.setItem(
			"aviary.chatDrafts",
			JSON.stringify({
				"assistant/s1": { text: "old", attachments: [], updated: 1 },
				"assistant/s2": {
					text: "fresh",
					attachments: [],
					updated: Date.now(),
				},
			}),
		);
	});
	await mockChats(page);
	await page.goto("/agents/assistant/chat/s1");
	await expect(messageBox(page)).toHaveValue("");
	const stored = await page.evaluate(() =>
		JSON.parse(localStorage.getItem("aviary.chatDrafts") ?? "{}"),
	);
	expect(Object.keys(stored)).toEqual(["assistant/s2"]);
});

/** Mocks one agent whose root files the Files page edits. */
async function mockFiles(page: Page) {
	const store = new Map([
		["AGENTS.md", "# Agents"],
		["PROFILE.md", "# Profile"],
	]);
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: { agents: [{ name: "assistant", model: "x/y", tasks: [] }] },
		config_save: "ok",
		config_validate: [],
		job_list: [],
		session_list: [],
		auth_list: [],
		tool_list: [],
		agent_file_list: () => Array.from(store.keys()).sort(),
		agent_file_read: (args) => store.get(String(args?.file ?? "")) ?? "",
		agent_file_write: (args) => {
			store.set(String(args?.file ?? ""), String(args?.content ?? ""));
			return "ok";
		},
	});
	return store;
}

test("leaving an edited file asks first; Stay keeps the edits, Leave goes", async ({
	page,
}) => {
	await mockFiles(page);
	await page.goto("/agents/assistant/files?file=AGENTS.md");
	const editor = page.getByTestId("agent-file-editor");
	await expect(editor).toHaveValue("# Agents");

	// Nothing changed: no prompt.
	await page.getByRole("link", { name: "Overview", exact: true }).click();
	await expect(page).toHaveURL(/\/system\/overview$/);
	await page.goBack();
	await expect(editor).toHaveValue("# Agents");

	await editor.fill("# Agents\nedited");
	const dialog = page.getByTestId("unsaved-changes-dialog");
	await page.getByRole("link", { name: "Overview", exact: true }).click();
	await expect(dialog).toBeVisible();
	await expect(dialog).toContainText(
		"You have unsaved changes. Leave this page?",
	);
	await dialog.getByRole("button", { name: "Stay" }).click();
	await expect(dialog).toHaveCount(0);
	await expect(page).toHaveURL(/\/agents\/assistant\/files\?file=AGENTS\.md$/);
	await expect(editor).toHaveValue("# Agents\nedited");

	// Opening another file discards the edits too, so it asks as well.
	await page
		.getByTestId("sidebar-nav")
		.getByRole("link", { name: "PROFILE.md" })
		.click();
	await expect(dialog).toBeVisible();
	await dialog.getByRole("button", { name: "Stay" }).click();
	await expect(editor).toHaveValue("# Agents\nedited");

	await page.getByRole("link", { name: "Overview", exact: true }).click();
	await dialog.getByRole("button", { name: "Leave" }).click();
	await expect(page).toHaveURL(/\/system\/overview$/);
});

test("the beforeunload handler is registered only while a field is dirty", async ({
	page,
}) => {
	await trackBeforeUnload(page);
	const store = await mockFiles(page);
	await page.goto("/agents/assistant/files?file=AGENTS.md");
	const editor = page.getByTestId("agent-file-editor");
	await expect(editor).toHaveValue("# Agents");
	const base = await beforeUnloadCount(page);

	await editor.fill("# Agents!");
	await expect.poll(() => beforeUnloadCount(page)).toBe(base + 1);

	// Undoing the edit back to the saved text is clean again.
	await editor.fill("# Agents");
	await expect.poll(() => beforeUnloadCount(page)).toBe(base);

	await editor.fill("# Agents saved");
	await expect.poll(() => beforeUnloadCount(page)).toBe(base + 1);
	await page
		.getByTestId("agent-file-bar")
		.getByRole("button", { name: "Save", exact: true })
		.click();
	await expect.poll(() => store.get("AGENTS.md")).toBe("# Agents saved");
	await expect.poll(() => beforeUnloadCount(page)).toBe(base);

	// After saving, leaving does not ask.
	await page.getByRole("link", { name: "Overview", exact: true }).click();
	await expect(page).toHaveURL(/\/system\/overview$/);
});

test("unsaved settings ask before leaving the Settings pages only", async ({
	page,
}) => {
	await trackBeforeUnload(page);
	await mockFiles(page);
	await page.goto("/system/overview");
	const nav = page.getByTestId("sidebar-nav");
	await expect(nav).toBeVisible();
	const base = await beforeUnloadCount(page);
	await nav.getByRole("link", { name: "Settings", exact: true }).click();
	await expect(page.getByTestId("settings-title")).toBeVisible();
	// A freshly loaded config is no unsaved change.
	const certPath = page.getByPlaceholder("/path/to/cert.pem");
	await expect(certPath).toBeVisible();
	expect(await beforeUnloadCount(page)).toBe(base);

	await certPath.fill("/tmp/cert.pem");
	await expect.poll(() => beforeUnloadCount(page)).toBe(base + 1);

	// Other Settings pages keep the draft, so moving between them does not ask.
	await nav
		.getByRole("link", { name: "Providers & Auth", exact: true })
		.click();
	await expect(page).toHaveURL(/\/system\/providers$/);
	await expect(page.getByTestId("unsaved-changes-dialog")).toHaveCount(0);
	await nav.getByRole("link", { name: "Settings", exact: true }).click();
	await expect(certPath).toHaveValue("/tmp/cert.pem");

	await nav.getByRole("link", { name: "Overview", exact: true }).click();
	await expect(page.getByTestId("unsaved-changes-dialog")).toBeVisible();
	await page.getByRole("button", { name: "Stay" }).click();
	await expect(page).toHaveURL(/\/system\/settings$/);
	await expect(certPath).toHaveValue("/tmp/cert.pem");
});
