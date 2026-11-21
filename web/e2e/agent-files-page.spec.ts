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
		agent_template_sync: (args?: Record<string, unknown>) => {
			synced.push(String(args?.agent ?? ""));
			store.set("SYSTEM.md", "# System");
			return "ok";
		},
	});
	return { store, synced, configSaves };
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
	for (const name of ["Refresh", "Sync Templates", "Save"])
		await expect(bar.getByRole("button", { name, exact: true })).toBeVisible();

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

test("the file bar's Save, Cmd/Ctrl+S, Refresh and Sync Templates work", async ({
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
	await bar.getByRole("button", { name: "Refresh", exact: true }).click();
	await expect(
		page.getByTestId("sidebar-files").getByTestId("file-row"),
	).toHaveText(["AGENTS.md", "NOTES.md"]);
	await expect(editor).toHaveValue("# Saved by shortcut");

	await bar
		.getByRole("button", { name: "Sync Templates", exact: true })
		.click();
	await expect.poll(() => synced).toEqual(["assistant"]);
	await expect(
		page.getByTestId("sidebar-files").getByTestId("file-row"),
	).toHaveText(["AGENTS.md", "NOTES.md", "SYSTEM.md"]);
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
