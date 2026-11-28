import { expect, test } from "vitest";
import { commands, type Locator, page, userEvent } from "vitest/browser";
import { currentURL, goBack, reload, renderApp } from "../component-test/app";
import { declarePages } from "../component-test/routes";
import { mockMCP } from "../component-test/server";
import { openGearMenuItem } from "./helpers/sidebar";

// The pages this file renders (see component-test/routes.ts).
declarePages(
	() => import("../views/ChatView.vue"),
	() => import("../views/OverviewView.vue"),
	() => import("../views/SettingsView.vue"),
);

// "stale stored drafts are dropped on load" is in
// unsaved-changes-stale-drafts: drafts are read from storage once, when
// their module loads, so it needs a fresh module.

const PIXEL_PNG = Uint8Array.from(
	atob(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
	),
	(c) => c.charCodeAt(0),
);

// Counts the window's beforeunload listeners, as the spec's init script
// did. The harness and app modules register listeners of their own, so
// tests compare against the count on load.
let beforeUnload = 0;
const add = window.addEventListener.bind(window);
const remove = window.removeEventListener.bind(window);
window.addEventListener = ((type: string, ...rest: never[]) => {
	if (type === "beforeunload") beforeUnload += 1;
	return (add as (...a: unknown[]) => void)(type, ...rest);
}) as typeof window.addEventListener;
window.removeEventListener = ((type: string, ...rest: never[]) => {
	if (type === "beforeunload") beforeUnload -= 1;
	return (remove as (...a: unknown[]) => void)(type, ...rest);
}) as typeof window.removeEventListener;

function beforeUnloadCount() {
	return beforeUnload;
}

function count(loc: Locator): number {
	return loc.elements().length;
}

/** The drafts the page stored for a reload to restore. */
function storedDrafts(): Record<
	string,
	{ text: string; attachments: unknown[] }
> {
	return JSON.parse(localStorage.getItem("aviary.chatDrafts") ?? "{}");
}

/** Mocks one agent with two chats, Main Chat and Research. */
function mockChats() {
	const runs: Array<Record<string, unknown>> = [];
	mockMCP({
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

function messageBox() {
	return page.getByTestId("chat-composer").getByRole("textbox");
}

test("a chat's draft is kept across route changes and reloads", async () => {
	mockChats();
	await renderApp("/agents/assistant/chat/s1");
	await expect.element(messageBox()).toBeEnabled();
	await expect.element(messageBox()).not.toHaveAttribute("readonly");
	const base = beforeUnloadCount();
	await messageBox().fill("half-typed thought");
	// The Attach files button opens the hidden file input; set its files as
	// the spec's file chooser did.
	await expect
		.element(page.getByRole("button", { name: "Attach files" }))
		.toBeEnabled();
	await userEvent.upload(
		page.getByTestId("chat-file-input"),
		new File([PIXEL_PNG], "shot.png", { type: "image/png" }),
	);
	await expect.poll(() => count(page.getByTestId("attachment-image"))).toBe(1);

	// Composers keep drafts instead of asking before leaving.
	expect(beforeUnloadCount()).toBe(base);
	await page.getByTestId("sidebar-system").click();
	await expect.poll(currentURL).toMatch(/\/system\/overview$/);
	expect(count(page.getByTestId("unsaved-changes-dialog"))).toBe(0);

	await goBack();
	await expect.element(messageBox()).toHaveValue("half-typed thought");
	await expect.poll(() => count(page.getByTestId("attachment-image"))).toBe(1);

	// A real reload reads the drafts back from storage; the module that
	// holds them survives reload() here, so check what a reload would read.
	await expect
		.poll(() => storedDrafts()["assistant/s1"]?.text)
		.toBe("half-typed thought");
	expect(storedDrafts()["assistant/s1"]?.attachments).toHaveLength(1);
	await reload();
	await expect.element(messageBox()).toHaveValue("half-typed thought");
	await expect.poll(() => count(page.getByTestId("attachment-image"))).toBe(1);
});

test("each chat keeps its own draft, cleared once sent", async () => {
	const runs = mockChats();
	await renderApp("/agents/assistant/chat/s1");
	await messageBox().fill("for main");

	const rows = page.getByTestId("sidebar-chats").getByTestId("chat-row");
	await rows.filter({ hasText: "Research" }).click();
	await expect.poll(currentURL).toMatch(/\/chat\/s2$/);
	await expect.element(messageBox()).toHaveValue("");
	await messageBox().fill("for research");

	await rows.filter({ hasText: "Main Chat" }).click();
	await expect.poll(currentURL).toMatch(/\/chat\/s1$/);
	await expect.element(messageBox()).toHaveValue("for main");

	(messageBox().element() as HTMLElement).focus();
	await commands.keyPress("Enter");
	await expect.poll(() => runs.length).toBe(1);
	await expect.element(messageBox()).toHaveValue("");

	await rows.filter({ hasText: "Research" }).click();
	await expect.element(messageBox()).toHaveValue("for research");
	await rows.filter({ hasText: "Main Chat" }).click();
	await expect.poll(currentURL).toMatch(/\/chat\/s1$/);
	await expect.element(messageBox()).toHaveValue("");

	// Nothing stored for a reload to bring back (see the test above).
	await expect.poll(() => storedDrafts()["assistant/s1"]).toBeUndefined();
	await reload();
	await expect.element(messageBox()).toHaveValue("");
});

/** Mocks one agent whose root files the Files page edits. */
function mockFiles() {
	const store = new Map([
		["AGENTS.md", "# Agents"],
		["PROFILE.md", "# Profile"],
	]);
	mockMCP({
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: { agents: [{ name: "assistant", model: "x/y", tasks: [] }] },
		config_save: () => "ok",
		config_validate: [],
		job_list: [],
		session_list: [],
		auth_list: [],
		tool_list: [],
		agent_file_list: () => Array.from(store.keys()).sort(),
		agent_file_read: (args?: Record<string, unknown>) =>
			store.get(String(args?.file ?? "")) ?? "",
		agent_file_write: (args?: Record<string, unknown>) => {
			store.set(String(args?.file ?? ""), String(args?.content ?? ""));
			return "ok";
		},
	});
	return store;
}

test("leaving an edited file asks first; Stay keeps the edits, Leave goes", async () => {
	mockFiles();
	await renderApp("/agents/assistant/files?file=AGENTS.md");
	const editor = page.getByTestId("agent-file-editor");
	await expect.element(editor).toHaveValue("# Agents");

	// Nothing changed: no prompt.
	await page.getByTestId("sidebar-system").click();
	await expect.poll(currentURL).toMatch(/\/system\/overview$/);
	await goBack();
	await expect.element(editor).toHaveValue("# Agents");

	await editor.fill("# Agents\nedited");
	const dialog = page.getByTestId("unsaved-changes-dialog");
	await page.getByTestId("sidebar-system").click();
	await expect.element(dialog).toBeVisible();
	await expect
		.element(dialog)
		.toMatchTextContent("You have unsaved changes. Leave this page?");
	await dialog.getByRole("button", { name: "Stay" }).click();
	await expect.poll(() => count(dialog)).toBe(0);
	await expect
		.poll(currentURL)
		.toMatch(/\/agents\/assistant\/files\?file=AGENTS\.md$/);
	await expect.element(editor).toHaveValue("# Agents\nedited");

	// Opening another file discards the edits too, so it asks as well.
	await page
		.getByTestId("sidebar-nav")
		.getByRole("link", { name: "PROFILE.md" })
		.click();
	await expect.element(dialog).toBeVisible();
	await dialog.getByRole("button", { name: "Stay" }).click();
	await expect.element(editor).toHaveValue("# Agents\nedited");

	await page.getByTestId("sidebar-system").click();
	await dialog.getByRole("button", { name: "Leave" }).click();
	await expect.poll(currentURL).toMatch(/\/system\/overview$/);
});

test("the beforeunload handler is registered only while a field is dirty", async () => {
	const store = mockFiles();
	await renderApp("/agents/assistant/files?file=AGENTS.md");
	const editor = page.getByTestId("agent-file-editor");
	await expect.element(editor).toHaveValue("# Agents");
	const base = beforeUnloadCount();

	await editor.fill("# Agents!");
	await expect.poll(beforeUnloadCount).toBe(base + 1);

	// Undoing the edit back to the saved text is clean again.
	await editor.fill("# Agents");
	await expect.poll(beforeUnloadCount).toBe(base);

	await editor.fill("# Agents saved");
	await expect.poll(beforeUnloadCount).toBe(base + 1);
	await page
		.getByTestId("agent-file-bar")
		.getByRole("button", { name: "Save", exact: true })
		.click();
	await expect.poll(() => store.get("AGENTS.md")).toBe("# Agents saved");
	await expect.poll(beforeUnloadCount).toBe(base);

	// After saving, leaving does not ask.
	await page.getByTestId("sidebar-system").click();
	await expect.poll(currentURL).toMatch(/\/system\/overview$/);
});

test("unsaved settings ask before leaving the Settings pages only", async () => {
	mockFiles();
	await renderApp("/system/overview");
	const nav = page.getByTestId("sidebar-nav");
	await expect.element(nav).toBeVisible();
	const base = beforeUnloadCount();
	await openGearMenuItem("Settings");
	await expect.element(page.getByTestId("settings-title")).toBeVisible();
	// A freshly loaded config is no unsaved change.
	const certPath = page.getByPlaceholder("/path/to/cert.pem");
	await expect.element(certPath).toBeVisible();
	expect(beforeUnloadCount()).toBe(base);

	await certPath.fill("/tmp/cert.pem");
	await expect.poll(beforeUnloadCount).toBe(base + 1);

	// Other Settings pages keep the draft, so moving between them does not ask.
	await page.getByTestId("settings-tab-providers").click();
	await expect.poll(currentURL).toMatch(/\/system\/providers$/);
	expect(count(page.getByTestId("unsaved-changes-dialog"))).toBe(0);
	await page.getByTestId("settings-tab-general").click();
	await expect.element(certPath).toHaveValue("/tmp/cert.pem");

	await nav.getByTestId("sidebar-system").click();
	await expect
		.element(page.getByTestId("unsaved-changes-dialog"))
		.toBeVisible();
	await page.getByRole("button", { name: "Stay" }).click();
	await expect.poll(currentURL).toMatch(/\/system\/settings$/);
	await expect.element(certPath).toHaveValue("/tmp/cert.pem");
});
