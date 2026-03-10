import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
});

test("task config can trigger a run-now job", async ({ page }) => {
	await mockMCP(page, {
		job_list: [],
		task_list: [
			{
				id: "assistant/daily-report",
				agent_id: "agent_assistant",
				agent_name: "assistant",
				name: "daily-report",
				trigger_type: "cron",
				schedule: "0 9 * * *",
				prompt: "write the report",
				channel: "last",
			},
		],
		task_run: {
			id: "job_12345678",
			task_id: "assistant/daily-report",
			agent_name: "assistant",
			status: "in_progress",
			attempts: 1,
			created_at: "",
			updated_at: "",
		},
	});

	await page.goto("/tasks");
	await page.getByRole("button", { name: "Run Now" }).click();

	await expect(
		page.getByText("Started assistant/daily-report as 12345678."),
	).toBeVisible();
});
