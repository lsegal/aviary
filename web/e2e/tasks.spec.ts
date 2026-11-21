import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
});

test("a configured task can be run now from System → Jobs", async ({
	page,
}) => {
	const runs: unknown[] = [];
	await mockMCP(page, {
		job_query: [],
		task_list: [
			{
				id: "assistant/daily-report",
				agent_id: "agent_assistant",
				agent_name: "assistant",
				name: "daily-report",
				trigger_type: "cron",
				schedule: "0 9 * * *",
				prompt: "write the report",
				target: "channel:alerts",
			},
		],
		task_run: (args) => {
			runs.push(args);
			return {
				id: "job_12345678",
				task_id: "assistant/daily-report",
				agent_id: "agent_assistant",
				status: "in_progress",
				attempts: 1,
				created_at: "",
				updated_at: "",
			};
		},
	});

	await page.goto("/system/jobs");
	await page.getByRole("button", { name: "Run Now" }).first().click();

	await expect.poll(() => runs).toEqual([{ name: "assistant/daily-report" }]);
});
