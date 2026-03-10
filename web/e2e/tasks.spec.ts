import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
});

test("task config can trigger a run-now job", async ({ page }) => {
	await mockMCP(page, {
		job_list: [],
		config_get: {
			server: {
				port: 16677,
				tls: { cert: "", key: "" },
				external_access: false,
				no_tls: false,
			},
			agents: [
				{
					name: "assistant",
					model: "test/model",
					fallbacks: [],
					channels: [],
					tasks: [
						{
							name: "daily-report",
							prompt: "write the report",
							schedule: "0 9 * * *",
						},
					],
				},
			],
			models: { providers: {}, defaults: { model: "", fallbacks: [] } },
			browser: { binary: "", cdp_port: 0 },
			scheduler: { concurrency: "auto" },
		},
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
