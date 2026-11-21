import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

/** Mocks one agent whose root files live in a store the UI reads and writes. */
async function mockFiles(page: Page, files: Record<string, string>) {
	const store = new Map(Object.entries(files));
	const synced: string[] = [];
	const configSaves: unknown[] = [];
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: { agents: [{ name: "assistant", model: "x/y", tasks: [] }] },
		config_save: (args) => {
			configSaves.push(args);
			return "ok";
		},
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
		agent_file_delete: (args) => {
			store.delete(String(args?.file ?? ""));
			return "ok";
		},
		agent_file_rename: (args) => {
			const from = String(args?.file ?? "");
			store.set(String(args?.to ?? ""), store.get(from) ?? "");
			store.delete(from);
			return "ok";
		},
		agent_template_sync: (args?: Record<string, unknown>) => {
			synced.push(String(args?.agent ?? ""));
			store.set("SYSTEM.md", "# System");
			return "ok";
		},
	});
	return { store, synced, configSaves };
}

/** Picks an action from the file bar's "..." menu, left of Save. */
async function fileMenu(page: Page, name: string) {
	await page.getByTestId("agent-file-menu").click();
	await page.getByRole("menuitem", { name, exact: true }).click();
}

const LONG = Array.from({ length: 300 }, (_, i) => `line ${i + 1}`).join("\n");

test("the Files page is one file bar over a full-page editor", async ({
	page,
}) => {
	await mockFiles(page, { "AGENTS.md": LONG, "PROFILE.md": "# Profile" });
	await page.goto("/agents/assistant/files?file=AGENTS.md");

	const bar = page.getByTestId("agent-file-bar");
	await expect(page.getByTestId("agent-file-name")).toHaveText("AGENTS.md");
	await expect(bar.getByTestId("agent-file-builtin")).toBeVisible();
	await expect(bar.getByTestId("agent-file-hint")).toHaveAttribute(
		"title",
		/built in and cannot be renamed or deleted/,
	);
	// The bar's actions are a "..." menu directly left of Save.
	const menu = bar.getByRole("button", { name: "More file actions" });
	const save = bar.getByRole("button", { name: "Save", exact: true });
	await expect(menu).toBeVisible();
	await expect(save).toBeVisible();
	for (const name of ["Refresh", "Sync Templates"])
		await expect(bar.getByRole("button", { name, exact: true })).toHaveCount(0);
	const [menuBox, saveBox] = await Promise.all([
		menu.boundingBox(),
		save.boundingBox(),
	]);
	expect((menuBox?.x ?? 0) + (menuBox?.width ?? 0)).toBeLessThanOrEqual(
		saveBox?.x ?? 0,
	);
	await menu.click();
	await expect(page.getByRole("menuitem")).toHaveText([
		"Refresh",
		"Sync Templates",
		"Rename",
		"Delete",
	]);
	// Built-in files cannot be renamed or deleted.
	for (const name of ["Rename", "Delete"])
		await expect(page.getByRole("menuitem", { name })).toHaveAttribute(
			"data-disabled",
			"",
		);
	await page.keyboard.press("Escape");

	// No settings page header or its duplicate actions.
	const main = page.locator("main");
	await expect(page.getByTestId("settings-title")).toHaveCount(0);
	await expect(main.getByRole("button", { name: "Reload" })).toHaveCount(0);
	await expect(main.getByRole("button", { name: "Save Changes" })).toHaveCount(
		0,
	);

	// The editor spans the page below the bar and scrolls on its own.
	const editor = page.getByTestId("agent-file-editor");
	await expect(editor).toHaveValue(LONG);
	const layout = await page.evaluate(() => {
		const bar = document.querySelector('[data-testid="agent-file-bar"]');
		const ed = document.querySelector<HTMLTextAreaElement>(
			'[data-testid="agent-file-editor"]',
		);
		const scroller = ed?.closest("main")?.querySelector(".h-full");
		if (!bar || !ed || !scroller) return null;
		const b = bar.getBoundingClientRect();
		const e = ed.getBoundingClientRect();
		return {
			gap: e.top - b.bottom,
			sameWidth: Math.abs(e.width - b.width) < 1,
			bottom: e.bottom,
			viewport: window.innerHeight,
			editorScrolls: ed.scrollHeight > ed.clientHeight,
			pageScrolls: scroller.scrollHeight > scroller.clientHeight,
		};
	});
	expect(layout).not.toBeNull();
	expect(layout?.gap).toBe(0);
	expect(layout?.sameWidth).toBe(true);
	expect(layout?.bottom).toBeCloseTo(layout?.viewport ?? 0, 0);
	expect(layout?.editorScrolls).toBe(true);
	expect(layout?.pageScrolls).toBe(false);

	// Non-built-in files get no badge.
	await page
		.getByTestId("sidebar-files")
		.getByTestId("file-row")
		.filter({ hasText: "PROFILE.md" })
		.click();
	await expect(page.getByTestId("agent-file-name")).toHaveText("PROFILE.md");
	await expect(page.getByTestId("agent-file-builtin")).toHaveCount(0);
});

test("the file bar's Save, Cmd/Ctrl+S, and its menu's Refresh and Sync Templates work", async ({
	page,
}) => {
	const { store, synced, configSaves } = await mockFiles(page, {
		"AGENTS.md": "# Agents",
	});
	await page.goto("/agents/assistant/files?file=AGENTS.md");
	const bar = page.getByTestId("agent-file-bar");
	const editor = page.getByTestId("agent-file-editor");
	await expect(editor).toHaveValue("# Agents");

	await editor.fill("# Saved by button");
	await bar.getByRole("button", { name: "Save", exact: true }).click();
	await expect(bar.getByText("Saved")).toBeVisible();
	await expect.poll(() => store.get("AGENTS.md")).toBe("# Saved by button");

	await editor.fill("# Saved by shortcut");
	await editor.press("ControlOrMeta+s");
	await expect.poll(() => store.get("AGENTS.md")).toBe("# Saved by shortcut");
	// The file save does not save the settings draft.
	expect(configSaves).toHaveLength(0);

	// Refresh reloads the file list from disk.
	store.set("NOTES.md", "# Notes");
	await fileMenu(page, "Refresh");
	await expect(
		page.getByTestId("sidebar-files").getByTestId("file-row"),
	).toHaveText(["AGENTS.md", "NOTES.md"]);
	await expect(editor).toHaveValue("# Saved by shortcut");

	await fileMenu(page, "Sync Templates");
	await expect.poll(() => synced).toEqual(["assistant"]);
	await expect(
		page.getByTestId("sidebar-files").getByTestId("file-row"),
	).toHaveText(["AGENTS.md", "NOTES.md", "SYSTEM.md"]);
});

test("Refresh re-reads the open file and asks before discarding edits", async ({
	page,
}) => {
	const { store } = await mockFiles(page, { "MEMORY.md": "# Memory v1" });
	await page.goto("/agents/assistant/files?file=MEMORY.md");
	const refresh = { click: () => fileMenu(page, "Refresh") };
	const menuButton = page.getByTestId("agent-file-menu");
	const editor = page.getByTestId("agent-file-editor");
	await expect(editor).toHaveValue("# Memory v1");

	// An agent edits the open file on disk; Refresh shows the new content.
	store.set("MEMORY.md", "# Memory v2");
	await refresh.click();
	await expect(editor).toHaveValue("# Memory v2");

	// With unsaved edits, declining the confirmation keeps them.
	store.set("MEMORY.md", "# Memory v3");
	await editor.fill("# My unsaved edit");
	const prompts: string[] = [];
	page.once("dialog", (dialog) => {
		prompts.push(dialog.message());
		void dialog.dismiss();
	});
	await refresh.click();
	await expect
		.poll(() => prompts)
		.toEqual(["Discard unsaved changes to MEMORY.md and reload it from disk?"]);
	await expect(menuButton).toBeEnabled();
	await expect(editor).toHaveValue("# My unsaved edit");

	// Accepting replaces them with the file on disk.
	page.once("dialog", (dialog) => void dialog.accept());
	await refresh.click();
	await expect(editor).toHaveValue("# Memory v3");

	// Once saved, edits are no longer unsaved, so Refresh does not ask.
	await editor.fill("# Saved edit");
	await editor.press("ControlOrMeta+s");
	await expect.poll(() => store.get("MEMORY.md")).toBe("# Saved edit");
	store.set("MEMORY.md", "# Memory v4");
	page.once("dialog", (dialog) => {
		prompts.push(dialog.message());
		void dialog.dismiss();
	});
	await refresh.click();
	await expect(editor).toHaveValue("# Memory v4");
	expect(prompts).toHaveLength(1);
});

test("the menu renames and deletes the open file", async ({ page }) => {
	const { store } = await mockFiles(page, {
		"AGENTS.md": "# Agents",
		"NOTES.md": "# Notes",
	});
	await page.goto("/agents/assistant/files?file=NOTES.md");
	const editor = page.getByTestId("agent-file-editor");
	const rows = page.getByTestId("sidebar-files").getByTestId("file-row");
	await expect(editor).toHaveValue("# Notes");

	await fileMenu(page, "Rename");
	const dialog = page.getByRole("dialog", { name: "Rename file" });
	await expect(dialog.getByLabel("File name")).toHaveValue("NOTES.md");
	await dialog.getByLabel("File name").fill("IDEAS");
	await dialog.getByRole("button", { name: "Save" }).click();
	await expect(dialog).toBeHidden();
	await expect(page).toHaveURL(/\?file=IDEAS\.md$/);
	await expect(page.getByTestId("agent-file-name")).toHaveText("IDEAS.md");
	await expect(editor).toHaveValue("# Notes");
	await expect(rows).toHaveText(["AGENTS.md", "IDEAS.md"]);
	expect(store.has("NOTES.md")).toBe(false);

	// Delete confirms with the file's name, then empties the editor.
	await fileMenu(page, "Delete");
	const confirm = page.getByRole("alertdialog", { name: "Delete file?" });
	await expect(confirm).toContainText("IDEAS.md");
	await confirm.getByRole("button", { name: "Delete" }).click();
	await expect(confirm).toBeHidden();
	await expect(rows).toHaveText(["AGENTS.md"]);
	expect(store.has("IDEAS.md")).toBe(false);
	await expect(page.getByTestId("agent-file-name")).toHaveText("Files");
	await expect(editor).toHaveValue("");
	await expect(editor).toHaveAttribute(
		"placeholder",
		"Pick a file in the sidebar, or add one with its +.",
	);

	// With no file open, Rename and Delete are unavailable.
	await page.getByTestId("agent-file-menu").click();
	for (const name of ["Rename", "Delete"])
		await expect(page.getByRole("menuitem", { name })).toHaveAttribute(
			"data-disabled",
			"",
		);
});

test("other agent pages keep the settings page header", async ({ page }) => {
	await mockFiles(page, { "AGENTS.md": "# Agents" });
	await page.goto("/agents/assistant/settings");
	await expect(page.getByTestId("settings-title")).toHaveText(
		"assistant · Agent Settings",
	);
	await expect(
		page.locator("main").getByRole("button", { name: "Save Changes" }),
	).toBeVisible();
	await expect(page.getByTestId("agent-file-bar")).toBeHidden();
});
