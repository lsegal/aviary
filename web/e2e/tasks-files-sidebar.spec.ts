import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// The rest of these scenarios are component tests now
// (web/src/scenarios/tasks-files-sidebar.component.test.ts). This one stays
// here because it needs a touch browser context, where (hover: none)
// matches; a component test can't emulate that.

interface Task {
	name: string;
	schedule?: string;
	prompt?: string;
	[key: string]: unknown;
}

interface Config {
	agents: Array<{
		name: string;
		model: string;
		tasks: Task[];
		channels?: Array<{ type: string; id: string; name?: string }>;
	}>;
	[key: string]: unknown;
}

/**
 * Mocks one agent with `tasks` and root `files`, keeping both as the UI
 * saves, renames and deletes them.
 */
async function mockAgent(
	page: Page,
	tasks: Task[],
	files: Record<string, string>,
	extra: Record<string, unknown> = {},
) {
	let config: Config = {
		agents: [
			{
				name: "assistant",
				model: "x/y",
				tasks,
				channels: [{ type: "slack", id: "standup", name: "Team standup" }],
			},
		],
		...extra,
	};
	const saved: Config[] = [];
	const deletedData: unknown[] = [];
	const converted: Record<string, unknown>[] = [];
	const previews: Record<string, unknown>[] = [];
	const taskSessions: Record<string, unknown>[] = [];
	const store = new Map(Object.entries(files));
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_save: (args) => {
			config = JSON.parse(String(args?.config ?? "{}")) as Config;
			saved.push(config);
			deletedData.push(args?.delete_data);
			return "ok";
		},
		config_task_rename: (args?: Record<string, unknown>) => {
			for (const t of config.agents[0].tasks)
				if (t.name === args?.task) t.name = String(args?.new_name);
			return "renamed";
		},
		config_task_convert_to_script: (args?: Record<string, unknown>) => {
			converted.push(args ?? {});
			return "task_compile_1";
		},
		config_validate: [],
		task_preview: (args?: Record<string, unknown>) => {
			previews.push(args ?? {});
			const at = (h: number) =>
				new Date(Date.now() + h * 3_600_000).toISOString();
			return { description: "preview", next: [at(1), at(2), at(3)] };
		},
		task_session: (args?: Record<string, unknown>) => {
			taskSessions.push(args ?? {});
			const name = String(args?.name);
			return { id: name, agent_id: "assistant", name, title: name };
		},
		session_messages: (args?: Record<string, unknown>) =>
			args?.session_id === "main"
				? []
				: [
						{
							id: "m1",
							role: "assistant",
							content: `${String(args?.session_id)}: all green`,
							timestamp: new Date().toISOString(),
						},
					],
		delete_preview: { sessions: 1, messages: 34, runs: 12, files: 3 },
		job_list: [],
		session_list: [
			{
				id: "main",
				agent_id: "assistant",
				name: "main",
				created_at: new Date().toISOString(),
			},
		],
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
	});
	return { saved, deletedData, store, converted, previews, taskSessions };
}

const FILES = {
	"AGENTS.md": "# Agents",
	"IDENTITY.md": "# Identity",
	"MEMORY.md": "Remembered",
};

function sidebarRow(page: Page, key: "tasks" | "files") {
	return page
		.getByTestId("sidebar-sole-agent")
		.getByTestId(`sidebar-row-${key}`);
}

function taskRows(page: Page) {
	return page.getByTestId("sidebar-tasks").getByTestId("task-row");
}

function fileRows(page: Page) {
	return page.getByTestId("sidebar-files").getByTestId("file-row");
}

test.describe("on a touch screen", () => {
	test.use({
		hasTouch: true,
		isMobile: true,
		viewport: { width: 1180, height: 820 },
	});

	test("+ and … are always visible", async ({ page }) => {
		await mockAgent(page, [{ name: "daily-briefing" }], FILES);
		await page.goto("/agents/assistant/files");
		await expect(
			sidebarRow(page, "files").getByRole("button", { name: "Add file" }),
		).toHaveCSS("opacity", "1");
		await expect(
			fileRows(page).first().getByTestId("file-menu-button"),
		).toHaveCSS("opacity", "1");

		await page.goto("/agents/assistant/tasks");
		await expect(
			sidebarRow(page, "tasks").getByRole("button", { name: "Add task" }),
		).toHaveCSS("opacity", "1");
		await expect(
			taskRows(page).first().getByTestId("task-menu-button"),
		).toHaveCSS("opacity", "1");
	});
});
