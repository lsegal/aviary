import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { mockMCP } from "../component-test/server";

// A precompiled task whose compiled script keeps failing keeps precompile on
// and shows a "Script failing" chip on its card, with a Recompile action
// (#1568).

function mockFailingTask() {
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
	mockMCP({
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

test("a failing compiled script shows Script failing with Recompile on its card", async () => {
	const recompiles = mockFailingTask();
	await renderApp("/agents/assistant/tasks");

	const card = page.locator('[data-testid=task-card][data-task="release"]');
	const chip = card.getByTestId("compiled-status-chip");
	await expect.element(chip).toHaveAttribute("data-state", "failing");
	await expect.element(chip).toHaveText("Script failing");
	await expect
		.element(chip)
		.toHaveAttribute("title", expect.stringMatching(/failed its last 3 runs/));
	await expect
		.element(chip)
		.toHaveAttribute("title", expect.stringMatching(/last error: gh 404/));

	// A task whose script works offers no Recompile on its card.
	const healthy = page.locator('[data-testid=task-card][data-task="healthy"]');
	await expect
		.element(healthy.getByTestId("compiled-status-chip"))
		.toHaveText("Compiled");
	await expect
		.poll(() => healthy.getByTestId("task-recompile").elements().length)
		.toBe(0);

	await card.getByTestId("task-recompile").click();
	await expect.element(chip).toHaveAttribute("data-state", "compiled");
	await expect
		.poll(() => card.getByTestId("task-recompile").elements().length)
		.toBe(0);
	expect(recompiles).toEqual([{ agent: "assistant", task: "release" }]);
});
