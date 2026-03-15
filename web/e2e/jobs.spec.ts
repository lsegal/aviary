import { expect, test } from "@playwright/test";
import { mockMCP, setAuthToken } from "./helpers/mockMCP";

test.beforeEach(async ({ page }) => {
	await setAuthToken(page);
});

test("scheduled tasks panel uses task_list data rather than job history", async ({
	page,
}) => {
	const jobs = Array.from({ length: 21 }, (_, index) => ({
		id: `job_${String(index + 1).padStart(8, "0")}`,
		task_id: `assistant/job-${index + 1}`,
		agent_id: "agent_assistant",
		agent_name: "assistant",
		prompt: `historical prompt ${index + 1}`,
		status: "completed",
		attempts: 1,
		max_retries: 3,
		created_at: "2026-03-14T09:00:00Z",
		updated_at: "2026-03-14T09:00:10Z",
	}));

	await mockMCP(page, {
		job_query: jobs,
		task_list: [
			{
				id: "assistant/task-1",
				agent_id: "agent_assistant",
				agent_name: "assistant",
				name: "task-1",
				trigger_type: "cron",
				schedule: "0 0 9 * * *",
				prompt: "configured task 1",
			},
			{
				id: "assistant/task-2",
				agent_id: "agent_assistant",
				agent_name: "assistant",
				name: "task-2",
				trigger_type: "cron",
				schedule: "0 0 10 * * *",
				prompt: "configured task 2",
			},
			{
				id: "assistant/task-3",
				agent_id: "agent_assistant",
				agent_name: "assistant",
				name: "task-3",
				trigger_type: "cron",
				schedule: "0 0 11 * * *",
				prompt: "configured task 3",
			},
			{
				id: "assistant/task-4",
				agent_id: "agent_assistant",
				agent_name: "assistant",
				name: "task-4",
				trigger_type: "cron",
				schedule: "0 0 12 * * *",
				prompt: "configured task 4",
			},
			{
				id: "assistant/task-5",
				agent_id: "agent_assistant",
				agent_name: "assistant",
				name: "task-5",
				trigger_type: "watch",
				watch: "notes/*.md",
				prompt: "configured task 5",
			},
		],
	});

	await page.goto("/jobs");

	await expect(page.getByText("21 jobs")).toBeVisible();
	await expect(page.getByText("5 configured")).toBeVisible();
	await expect(page.getByText("assistant/task-1")).toBeVisible();
	await expect(page.getByText("assistant/task-5")).toBeVisible();
	await expect(page.locator("text=job_00000001")).toHaveCount(0);
});

test("initial jobs page load sequences MCP requests", async ({ page }) => {
	await page.route("/api/login", (route) =>
		route.fulfill({ status: 200, contentType: "application/json", body: "{}" }),
	);

	let jobQueryActive = false;

	await page.route("/mcp", async (route) => {
		const body = route.request().postDataJSON() as {
			id?: number;
			method: string;
			params?: { name?: string };
		};

		if (body.method === "initialize") {
			return route.fulfill({
				status: 200,
				headers: {
					"Content-Type": "application/json",
					"Mcp-Session-Id": "mock-session",
				},
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: {
						protocolVersion: "2024-11-05",
						capabilities: {},
						serverInfo: { name: "aviary-mock", version: "0.0.0" },
					},
				}),
			});
		}

		if (body.method === "notifications/initialized") {
			return route.fulfill({ status: 200, body: "{}" });
		}

		if (body.method !== "tools/call") {
			return route.fulfill({ status: 200, body: "{}" });
		}

		if (body.params?.name === "job_query") {
			jobQueryActive = true;
			await new Promise((resolve) => setTimeout(resolve, 150));
			jobQueryActive = false;
			return route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: { content: [{ type: "text", text: "[]" }] },
				}),
			});
		}

		if (body.params?.name === "task_list") {
			expect(jobQueryActive).toBeFalsy();
			return route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: { content: [{ type: "text", text: "[]" }] },
				}),
			});
		}

		return route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				jsonrpc: "2.0",
				id: body.id,
				result: { content: [{ type: "text", text: "[]" }] },
			}),
		});
	});

	await page.goto("/jobs");

	await expect(page.getByText("0 jobs")).toBeVisible();
	await expect(page.getByText("0 configured")).toBeVisible();
});
