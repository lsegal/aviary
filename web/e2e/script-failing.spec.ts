import { expect, type Page, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

// A precompiled task whose compiled script keeps failing keeps precompile on
// and shows a "Script failing" chip on its card, with a Recompile action
// (#1568).

async function mockFailingTask(page: Page) {
	const task: Record<string, unknown> = {
		name: "release",
		type: "project",
		project: "acme/app",
		every: "5m",
		prompt: "Release when main moves",
		precompile: true,
		compiled: 'print("release")',
		compiled_key: "k",
		script_verified: true,
		script_failures: 3,
		script_last_error: "gh 404",
	};
	const healthy = { ...task, name: "healthy", script_failures: undefined };
	const recompiles: Record<string, unknown>[] = [];
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => ({
			agents: [{ name: "assistant", model: "x/y", tasks: [task, healthy] }],
		}),
		config_validate: [],
		auth_list: [],
		tool_list: [],
		session_list: [],
		job_query: [],
		job_list: [],
		task_compile_query: [],
		task_preview: () => ({ description: "every 5m0s (UTC)", next: [] }),
		config_task_convert_to_script: (args?: Record<string, unknown>) => {
			recompiles.push(args ?? {});
			task.script_failures = undefined;
			task.script_last_error = undefined;
			task.script_verified = undefined;
			task.compiled = 'print("fresh")';
			task.compiled_key = "fresh";
			return `recompiling task "${String(args?.task)}"`;
		},
	});
	return recompiles;
}

test("a failing compiled script shows Script failing with Recompile on its card", async ({
	page,
}) => {
	const recompiles = await mockFailingTask(page);
	await page.goto("/agents/assistant/tasks");

	const card = page.locator('[data-testid=task-card][data-task="release"]');
	const chip = card.getByTestId("compiled-status-chip");
	await expect(chip).toHaveAttribute("data-state", "failing");
	await expect(chip).toHaveText("Script failing");
	await expect(chip).toHaveAttribute("title", /failed its last 3 runs/);
	await expect(chip).toHaveAttribute("title", /last error: gh 404/);

	// A task whose script works offers no Recompile on its card.
	const healthy = page.locator('[data-testid=task-card][data-task="healthy"]');
	await expect(healthy.getByTestId("compiled-status-chip")).toHaveText(
		"Compiled",
	);
	await expect(healthy.getByTestId("task-recompile")).toHaveCount(0);

	await card.getByTestId("task-recompile").click();
	await expect(chip).toHaveAttribute("data-state", "compiled");
	await expect(card.getByTestId("task-recompile")).toHaveCount(0);
	expect(recompiles).toEqual([{ agent: "assistant", task: "release" }]);
});
