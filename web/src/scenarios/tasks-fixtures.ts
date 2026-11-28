// Fixtures shared by the tasks component tests (#1636): an agent with three
// tasks and their jobs, and the run details drawer's controls.
import { expect } from "vitest";
import { commands, page } from "vitest/browser";
import { mockMCP } from "../component-test/server";

export const HOUR = 3_600_000;
export const iso = (offset: number) =>
	new Date(Date.now() + offset).toISOString();

export function job(
	n: number,
	task: string,
	status: string,
	ageMs: number,
	durationMs = 2_000,
) {
	return {
		id: `job_${String(n).padStart(8, "0")}`,
		task_id: `assistant/${task}`,
		agent_id: "assistant",
		prompt: "",
		status,
		attempts: 1,
		max_retries: 3,
		created_at: iso(-ageMs),
		updated_at: iso(-ageMs + durationMs),
	};
}

/**
 * Mocks an agent with three tasks: a daily one that succeeded, a paused
 * weekly one that failed, and a project task running now.
 */
export function mockTasks() {
	const config = {
		agents: [
			{
				name: "assistant",
				model: "x/y",
				tasks: [
					{
						name: "daily-report",
						trigger: "daily",
						at: "02:00",
						prompt: "write the report",
					},
					{
						name: "weekly-cleanup",
						trigger: "weekly",
						days: "mon",
						at: "09:00",
						enabled: false,
						prompt: "clean up",
					},
					{
						name: "ci-watch",
						type: "project",
						project: "o/r",
						every: "1h",
						prompt: "watch ci",
					},
				] as Record<string, unknown>[],
			},
		],
	};
	const calls = {
		runs: [] as unknown[],
		updates: [] as Record<string, unknown>[],
	};
	mockMCP({
		agent_list: [
			{ id: "assistant", name: "assistant", model: "x/y", state: "idle" },
		],
		config_get: () => config,
		config_validate: [],
		auth_list: [],
		tool_list: [],
		session_list: [],
		job_query: [
			job(4, "ci-watch", "in_progress", 60_000),
			job(3, "weekly-cleanup", "failed", 2 * HOUR, 4_000),
			job(2, "daily-report", "completed", 3 * HOUR, 1_000),
			job(1, "daily-report", "completed", 27 * HOUR, 3_000),
		],
		task_compile_query: [
			{
				id: "task_compile_1",
				agent_id: "assistant",
				task_name: "daily-report",
				status: "succeeded",
				created_at: iso(-HOUR),
				updated_at: iso(-HOUR),
			},
		],
		task_preview: (args?: Record<string, unknown>) => {
			const name = String(args?.name);
			if (name.endsWith("/daily-report"))
				return {
					description: "daily at 02:00 (America/Toronto)",
					next: [iso(41 * 60_000), iso(25 * HOUR)],
				};
			if (name.endsWith("/weekly-cleanup"))
				return { description: "weekly on Mon at 09:00 (UTC)", next: [] };
			return {
				description: "every 1h0m0s (UTC)",
				next: [iso(HOUR / 2), iso(1.5 * HOUR)],
			};
		},
		task_run: (args?: unknown) => {
			calls.runs.push(args);
			return job(5, "daily-report", "in_progress", 0);
		},
		task_update: (args?: Record<string, unknown>) => {
			calls.updates.push(args ?? {});
			const task = config.agents[0].tasks.find(
				(t) => `assistant/${t.name}` === args?.name,
			);
			if (task && typeof args?.enabled === "boolean")
				task.enabled = args.enabled;
			return "updated";
		},
	});
	return calls;
}

// The run details drawer resizes from its left edge and remembers its width
// per browser; narrow viewports keep the fixed layout (#878).

export async function openRunDetails() {
	await page.getByTestId("more-analytics-toggle").click();
	await page
		.locator("tbody tr")
		.filter({ hasText: "weekly-cleanup" })
		.first()
		.click();
	await expect.element(page.getByTestId("details-panel")).toBeVisible();
	// The drawer slides in from the right; its 8px resize handle only sits
	// where a drag can hit it once the drawer is flush with the viewport edge.
	await expect
		.poll(() => {
			const box = page
				.getByTestId("details-panel")
				.element()
				.getBoundingClientRect();
			return Math.round(box.x + box.width);
		})
		.toBe(window.innerWidth);
}

export function panelWidth(): number {
	const el = page.getByTestId("details-panel").query();
	return Math.round(el?.getBoundingClientRect().width ?? 0);
}

export async function dragPanelEdge(dx: number) {
	const box = page
		.getByTestId("details-panel-resize")
		.element()
		.getBoundingClientRect();
	const x = box.x + box.width / 2;
	const y = box.y + box.height / 2;
	await commands.mouseMove(x, y);
	await commands.mouseDown();
	await commands.mouseMove(x + dx / 2, y);
	await commands.mouseMove(x + dx, y);
	await commands.mouseUp();
}
