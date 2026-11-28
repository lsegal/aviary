import { expect, test, vi } from "vitest";
import { commands, type Locator, page } from "vitest/browser";
import { currentURL, goto, reload, renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";

/** Mocks one agent whose root files live in a store the UI reads and writes. */
function mockFiles(files: Record<string, string>) {
	const store = new Map(Object.entries(files));
	const synced: string[] = [];
	const configSaves: unknown[] = [];
	mockMCP({
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: { agents: [{ name: "assistant", model: "x/y", tasks: [] }] },
		config_save: (args?: unknown) => {
			configSaves.push(args);
			return "ok";
		},
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
		agent_file_delete: (args?: Record<string, unknown>) => {
			store.delete(String(args?.file ?? ""));
			return "ok";
		},
		agent_file_rename: (args?: Record<string, unknown>) => {
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

/** Each matched element's text, whitespace collapsed, as Playwright's
 * toHaveText with an array compares them. */
function texts(loc: Locator): string[] {
	return loc
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
}

function count(loc: Locator): number {
	return loc.elements().length;
}

/** Focuses `loc` and presses `key`, as Playwright's locator.press. */
async function press(loc: Locator, key: string) {
	(loc.element() as HTMLElement).focus();
	await commands.keyPress(key);
}

/**
 * Runs one of the file's actions in the page header (#1147): Refresh and
 * Sync Templates are buttons; Rename and Delete sit in its "..." menu.
 */
async function fileMenu(name: string) {
	if (name === "Rename" || name === "Delete") {
		await page.getByTestId("agent-file-menu").click();
		await page.getByRole("menuitem", { name, exact: true }).click();
		return;
	}
	await page
		.getByTestId("agent-file-bar")
		.getByRole("button", { name, exact: true })
		.click();
}

const LONG = Array.from({ length: 300 }, (_, i) => `line ${i + 1}`).join("\n");

test("the Files page is the shared page header over a full-page editor (#1147)", async () => {
	mockFiles({ "AGENTS.md": LONG, "PROFILE.md": "# Profile" });
	await renderApp("/agents/assistant/files?file=AGENTS.md");

	// The file's name is the header's title; the built-in badge is its status.
	const header = page.getByTestId("settings-header");
	const bar = page.getByTestId("agent-file-bar");
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("AGENTS.md");
	await expect.element(header.getByTestId("agent-file-builtin")).toBeVisible();
	await expect
		.element(header.getByTestId("agent-file-hint"))
		.toHaveAttribute(
			"title",
			expect.stringMatching(/built in and cannot be renamed or deleted/),
		);
	// Refresh, Sync Templates and Save are labeled actions, then the
	// Preview toggle and the "..." menu with Rename and Delete.
	await expect
		.poll(() => texts(bar.locator(":scope > button")))
		.toEqual(["Refresh", "Sync Templates", "Save", "Preview", ""]);
	await bar.getByRole("button", { name: "More file actions" }).click();
	await expect
		.poll(() => texts(page.getByRole("menuitem")))
		.toEqual(["Rename", "Delete"]);
	// Built-in files cannot be renamed or deleted.
	for (const name of ["Rename", "Delete"])
		await expect
			.element(page.getByRole("menuitem", { name }))
			.toHaveAttribute("data-disabled", "");
	await commands.keyPress("Escape");

	// No settings actions.
	const main = page.locator("main");
	expect(count(main.getByRole("button", { name: "Reload" }))).toBe(0);
	expect(count(main.getByRole("button", { name: "Save Changes" }))).toBe(0);

	// The editor spans the page below the bar and scrolls on its own.
	const editor = page.getByTestId("agent-file-editor");
	await expect.element(editor).toHaveValue(LONG);
	const layout = (() => {
		const bar = document.querySelector('[data-testid="settings-header"]');
		const ed = document.querySelector<HTMLTextAreaElement>(
			'[data-testid="agent-file-editor"]',
		);
		const split = document.querySelector('[data-testid="file-preview-split"]');
		const scroller = document.querySelector('[data-testid="settings-scroll"]');
		if (!bar || !ed || !split || !scroller) return null;
		const b = bar.getBoundingClientRect();
		const e = ed.getBoundingClientRect();
		// The editor and the Preview panel beside it span the bar's width.
		return {
			gap: e.top - b.bottom,
			sameWidth: Math.abs(split.getBoundingClientRect().width - b.width) < 1,
			bottom: e.bottom,
			viewport: window.innerHeight,
			editorScrolls: ed.scrollHeight > ed.clientHeight,
			pageScrolls: scroller.scrollHeight > scroller.clientHeight,
		};
	})();
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
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("PROFILE.md");
	await expect
		.poll(() => count(page.getByTestId("agent-file-builtin")))
		.toBe(0);
});

test("the header's Save, Cmd/Ctrl+S, Refresh and Sync Templates work", async () => {
	const { store, synced, configSaves } = mockFiles({
		"AGENTS.md": "# Agents",
	});
	await renderApp("/agents/assistant/files?file=AGENTS.md");
	const bar = page.getByTestId("agent-file-bar");
	const editor = page.getByTestId("agent-file-editor");
	await expect.element(editor).toHaveValue("# Agents");

	await editor.fill("# Saved by button");
	await bar.getByRole("button", { name: "Save", exact: true }).click();
	await expect.element(bar.getByText("Saved")).toBeVisible();
	await expect.poll(() => store.get("AGENTS.md")).toBe("# Saved by button");

	await editor.fill("# Saved by shortcut");
	await press(editor, "ControlOrMeta+s");
	await expect.poll(() => store.get("AGENTS.md")).toBe("# Saved by shortcut");
	// The file save does not save the settings draft.
	expect(configSaves).toHaveLength(0);

	// Refresh reloads the file list from disk.
	store.set("NOTES.md", "# Notes");
	await fileMenu("Refresh");
	const rows = page.getByTestId("sidebar-files").getByTestId("file-row");
	await expect.poll(() => texts(rows)).toEqual(["AGENTS.md", "NOTES.md"]);
	await expect.element(editor).toHaveValue("# Saved by shortcut");

	await fileMenu("Sync Templates");
	await expect.poll(() => synced).toEqual(["assistant"]);
	await expect
		.poll(() => texts(rows))
		.toEqual(["AGENTS.md", "NOTES.md", "SYSTEM.md"]);
});

test("Refresh re-reads the open file and asks before discarding edits", async () => {
	const { store } = mockFiles({ "MEMORY.md": "# Memory v1" });
	await renderApp("/agents/assistant/files?file=MEMORY.md");
	const refresh = { click: () => fileMenu("Refresh") };
	const menuButton = page
		.getByTestId("agent-file-bar")
		.getByRole("button", { name: "Refresh", exact: true });
	const editor = page.getByTestId("agent-file-editor");
	await expect.element(editor).toHaveValue("# Memory v1");

	// An agent edits the open file on disk; Refresh shows the new content.
	store.set("MEMORY.md", "# Memory v2");
	await refresh.click();
	await expect.element(editor).toHaveValue("# Memory v2");

	// With unsaved edits, it asks in the app's own modal (#1311); keeping
	// the edits closes it. No native dialog is ever shown.
	const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
	const alert = vi.spyOn(window, "alert").mockReturnValue();
	const prompt_ = vi.spyOn(window, "prompt").mockReturnValue(null);
	store.set("MEMORY.md", "# Memory v3");
	await editor.fill("# My unsaved edit");
	const prompt = page.getByTestId("discard-file-dialog");
	await refresh.click();
	await expect.element(prompt).toHaveAccessibleName("Discard unsaved changes?");
	await expect
		.element(prompt)
		.toMatchTextContent(
			"Discard unsaved changes to MEMORY.md and reload it from disk?",
		);
	await prompt.getByRole("button", { name: "Keep editing" }).click();
	await expect.poll(() => count(prompt)).toBe(0);
	await expect.element(menuButton).toBeEnabled();
	await expect.element(editor).toHaveValue("# My unsaved edit");

	// Confirming replaces them with the file on disk.
	await refresh.click();
	await prompt.getByRole("button", { name: "Discard and reload" }).click();
	await expect.poll(() => count(prompt)).toBe(0);
	await expect.element(editor).toHaveValue("# Memory v3");

	// Once saved, edits are no longer unsaved, so Refresh does not ask.
	await editor.fill("# Saved edit");
	await press(editor, "ControlOrMeta+s");
	await expect.poll(() => store.get("MEMORY.md")).toBe("# Saved edit");
	store.set("MEMORY.md", "# Memory v4");
	await refresh.click();
	await expect.element(editor).toHaveValue("# Memory v4");
	expect(count(prompt)).toBe(0);
	expect(confirm).not.toHaveBeenCalled();
	expect(alert).not.toHaveBeenCalled();
	expect(prompt_).not.toHaveBeenCalled();
	vi.restoreAllMocks();
});

test("the menu renames and deletes the open file", async () => {
	const { store } = mockFiles({
		"AGENTS.md": "# Agents",
		"NOTES.md": "# Notes",
	});
	await renderApp("/agents/assistant/files?file=NOTES.md");
	const editor = page.getByTestId("agent-file-editor");
	const rows = page.getByTestId("sidebar-files").getByTestId("file-row");
	await expect.element(editor).toHaveValue("# Notes");

	await fileMenu("Rename");
	const dialog = page.getByRole("dialog", { name: "Rename file" });
	await expect
		.element(dialog.getByLabelText("File name"))
		.toHaveValue("NOTES.md");
	await dialog.getByLabelText("File name").fill("IDEAS");
	await dialog.getByRole("button", { name: "Save" }).click();
	await expect.element(dialog).not.toBeInTheDocument();
	await expect.poll(currentURL).toMatch(/\?file=IDEAS\.md$/);
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("IDEAS.md");
	await expect.element(editor).toHaveValue("# Notes");
	await expect.poll(() => texts(rows)).toEqual(["AGENTS.md", "IDEAS.md"]);
	expect(store.has("NOTES.md")).toBe(false);

	// Delete confirms with the file's name, then empties the editor.
	await fileMenu("Delete");
	const confirm = page.getByRole("alertdialog", { name: "Delete file?" });
	await expect.element(confirm).toMatchTextContent("IDEAS.md");
	await confirm.getByRole("button", { name: "Delete" }).click();
	await expect.element(confirm).not.toBeInTheDocument();
	await expect.poll(() => texts(rows)).toEqual(["AGENTS.md"]);
	expect(store.has("IDEAS.md")).toBe(false);
	await expect.element(page.getByTestId("settings-title")).toHaveText("Files");
	await expect.element(editor).toHaveValue("");
	await expect
		.element(editor)
		.toHaveAttribute(
			"placeholder",
			"Pick a file in the sidebar, or add one with its +.",
		);

	// With no file open, Rename and Delete are unavailable.
	await page.getByTestId("agent-file-menu").click();
	for (const name of ["Rename", "Delete"])
		await expect
			.element(page.getByRole("menuitem", { name }))
			.toHaveAttribute("data-disabled", "");
});

test("other agent pages keep the settings page header", async () => {
	mockFiles({ "AGENTS.md": "# Agents" });
	await renderApp("/agents/assistant/settings");
	await expect
		.element(page.getByTestId("settings-title"))
		.toHaveText("Agent Settings");
	await expect
		.element(page.locator("main").getByRole("button", { name: "Save Changes" }))
		.toBeVisible();
	expect(count(page.getByTestId("agent-file-bar"))).toBe(0);
});

const PREVIEW_MD = [
	"# Agent rules",
	"",
	"Be **brief** and use `tools`.",
	"",
	"- one",
	"- two",
	"",
	"<script>alert(1)</script>",
].join("\n");

/** The editor and Preview panel's widths, and the bar's, in pixels. */
function splitWidths() {
	const width = (id: string) =>
		document.querySelector(`[data-testid="${id}"]`)?.getBoundingClientRect()
			.width ?? 0;
	return {
		bar: width("settings-header"),
		editor: width("agent-file-editor"),
		preview: width("file-preview-pane"),
	};
}

test("a Markdown file opens with a live rendered Preview beside the editor", async () => {
	mockFiles({ "AGENTS.md": PREVIEW_MD });
	await renderApp("/agents/assistant/files?file=AGENTS.md");

	const editor = page.getByTestId("agent-file-editor");
	const preview = page.getByTestId("file-preview");
	const toggle = page
		.getByTestId("agent-file-bar")
		.getByRole("button", { name: "Preview" });
	await expect.element(toggle).toHaveAttribute("aria-pressed", "true");
	await expect.element(preview).toBeVisible();
	await expect
		.element(preview)
		.toHaveAttribute("aria-label", "Markdown preview");
	// Rendered with chat's renderer and prose styles; raw HTML stays text.
	const md = page.getByTestId("markdown-preview");
	await expect.element(md).toHaveClass(/prose/);
	await expect
		.element(md.getByRole("heading", { level: 1 }))
		.toHaveText("Agent rules");
	await expect.element(md.locator("strong")).toHaveText("brief");
	await expect.element(md.locator("code")).toHaveText("tools");
	await expect
		.poll(() => texts(md.getByRole("listitem")))
		.toEqual(["one", "two"]);
	expect(count(md.locator("script"))).toBe(0);
	await expect.element(md).toMatchTextContent("<script>alert(1)</script>");

	// Editor left, preview right.
	const e = editor.element().getBoundingClientRect();
	const p = preview.element().getBoundingClientRect();
	expect(e.x + e.width).toBeLessThanOrEqual(p.x);

	// Typing updates the preview without saving.
	await editor.fill("## Edited heading\n\n_live_");
	await expect
		.element(md.getByRole("heading", { level: 2 }))
		.toHaveText("Edited heading");
	await expect.element(md.locator("em")).toHaveText("live");
	await expect.poll(() => count(md.getByRole("heading", { level: 1 }))).toBe(0);
});

test("dragging the divider resizes the panes within their minimums and is remembered", async () => {
	mockFiles({ "AGENTS.md": PREVIEW_MD });
	await renderApp("/agents/assistant/files?file=AGENTS.md");
	const divider = page.getByRole("separator", { name: "Resize preview" });
	await expect.element(divider).toBeVisible();
	const before = splitWidths();

	const drag = async (dx: number) => {
		const box = divider.element().getBoundingClientRect();
		const x = box.x + box.width / 2;
		const y = box.y + box.height / 2;
		await commands.mouseMove(x, y);
		await commands.mouseDown();
		await commands.mouseMove(x + dx, y, 5);
		await commands.mouseUp();
	};

	// Dragging left widens the preview and narrows the editor.
	await drag(-100);
	const wider = splitWidths();
	expect(wider.preview).toBeCloseTo(before.preview + 100, -1);
	expect(wider.editor).toBeCloseTo(before.editor - 100, -1);

	// Neither pane shrinks below its minimum.
	await drag(5000);
	expect(splitWidths().preview).toBeCloseTo(240, 0);
	await drag(-5000);
	expect(splitWidths().editor).toBeGreaterThanOrEqual(279);

	// The keyboard moves the divider too.
	await press(divider, "End");
	await expect.poll(() => splitWidths().preview).toBeCloseTo(240, 0);
	await commands.keyPress("ArrowLeft");
	await expect.element(divider).toHaveAttribute("aria-valuenow", "256");

	await reload();
	await expect.element(page.getByTestId("file-preview")).toBeVisible();
	await expect.poll(() => splitWidths().preview).toBeCloseTo(256, 0);
});

test("the header's Preview toggle collapses and expands the panel and is remembered", async () => {
	mockFiles({ "AGENTS.md": PREVIEW_MD });
	await renderApp("/agents/assistant/files?file=AGENTS.md");
	const toggle = page
		.getByTestId("agent-file-bar")
		.getByRole("button", { name: "Preview" });
	const preview = page.getByTestId("file-preview");
	await expect.element(preview).toBeVisible();

	await toggle.click();
	await expect.element(toggle).toHaveAttribute("aria-pressed", "false");
	await expect.poll(() => count(preview)).toBe(0);
	let widths = splitWidths();
	expect(widths.editor).toBeCloseTo(widths.bar, 0);

	await reload();
	await expect
		.element(page.getByTestId("agent-file-editor"))
		.toHaveValue(PREVIEW_MD);
	await expect.element(toggle).toHaveAttribute("aria-pressed", "false");
	expect(count(preview)).toBe(0);

	// The toggle is a keyboard-operable button.
	await press(toggle, "Enter");
	await expect.element(toggle).toHaveAttribute("aria-pressed", "true");
	await expect.element(preview).toBeVisible();
	await reload();
	await expect.element(preview).toBeVisible();
	widths = splitWidths();
	expect(widths.editor + widths.preview).toBeCloseTo(widths.bar, 0);
});

test("a file with no previewer has no Preview toggle and a full-width editor", async () => {
	mockFiles({ "AGENTS.md": "# Agents", "notes.txt": "plain" });
	await renderApp("/agents/assistant/files?file=notes.txt");
	const editor = page.getByTestId("agent-file-editor");
	await expect.element(editor).toHaveValue("plain");
	expect(count(page.getByTestId("file-preview-toggle"))).toBe(0);
	expect(count(page.getByTestId("file-preview"))).toBe(0);
	expect(count(page.getByTestId("file-preview-divider"))).toBe(0);
	const bar = page
		.getByTestId("settings-header")
		.element()
		.getBoundingClientRect();
	const ed = editor.element().getBoundingClientRect();
	expect(ed.width).toBeCloseTo(bar.width, 0);
});

test("at mobile width the Preview is an Edit / Preview tab, never a squeezed split", async () => {
	await page.viewport(390, 844);
	mockFiles({ "AGENTS.md": PREVIEW_MD, "notes.txt": "plain" });
	await renderApp("/agents/assistant/files?file=AGENTS.md");
	const editor = page.getByTestId("agent-file-editor");
	const preview = page.getByTestId("file-preview");
	const tabs = page.getByRole("tablist", { name: "Editor view" });
	await expect.element(editor).toHaveValue(PREVIEW_MD);
	const toggle = page.getByTestId("file-preview-toggle");
	if (count(toggle)) await expect.element(toggle).not.toBeVisible();
	expect(count(page.getByTestId("file-preview-divider"))).toBe(0);
	const edit = tabs.getByRole("tab", { name: "Edit" });
	const previewTab = tabs.getByRole("tab", { name: "Preview" });
	await expect.element(edit).toHaveAttribute("aria-selected", "true");
	expect(count(preview)).toBe(0);

	await previewTab.click();
	await expect.element(previewTab).toHaveAttribute("aria-selected", "true");
	if (count(editor)) await expect.element(editor).not.toBeVisible();
	await expect
		.element(
			page.getByTestId("markdown-preview").getByRole("heading", { level: 1 }),
		)
		.toHaveText("Agent rules");
	const widths = {
		scroll: document.documentElement.scrollWidth,
		viewport: window.innerWidth,
		preview:
			document
				.querySelector('[data-testid="file-preview"]')
				?.getBoundingClientRect().width ?? 0,
	};
	expect(widths.scroll).toBeLessThanOrEqual(widths.viewport);
	expect(widths.preview).toBeCloseTo(widths.viewport, 0);

	await edit.click();
	await expect.element(editor).toBeVisible();
	await expect.poll(() => count(preview)).toBe(0);

	// Files without a previewer get no tabs.
	await goto("/agents/assistant/files?file=notes.txt");
	await expect.element(editor).toHaveValue("plain");
	await expect.poll(() => count(tabs)).toBe(0);
});
