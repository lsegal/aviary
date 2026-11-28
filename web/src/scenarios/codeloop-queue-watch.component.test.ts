import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { mockMCP, route } from "../component-test/server";

// The `aviary watch` session case of codeloop-queue (#1095), in its own
// file: the signed-out health check it loads is module state that would
// carry into other tests.

test("a watch session's one-project board offers the Queue menu", async () => {
	mockMCP({
		agent_list: [{ name: "watch" }],
		job_list: [],
		task_list: [],
		task_upcoming: [],
		config_validate: [],
		config_get: {
			agents: [{ name: "watch", projects: [{ target: "owner/repo" }] }],
		},
		project_status: {
			states: { starting: 0, paused: 0, running: 0, done: 0, failed: 0 },
			targets: ["owner/repo"],
		},
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
		project_queue_candidates: {
			target: "owner/repo",
			repository: "owner/repo",
			pull_requests: [],
		},
	});
	// After mockMCP, whose own /api/health route it overrides.
	route("/api/health", (r) =>
		r.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				ok: true,
				version: "dev",
				auth_disabled: true,
				minimal: true,
			}),
		}),
	);
	await renderApp("/agents/watch/projects", { token: null });
	await page.getByTestId("queue-menu").click();
	await page.getByTestId("queue-gh-safe-merge").click();
	await expect
		.element(page.getByTestId("queue-dialog"))
		.toMatchTextContent("No open pull requests");
});
