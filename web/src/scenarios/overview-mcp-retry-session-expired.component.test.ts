import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { renderApp } from "../component-test/app";
import { mockMCP, route } from "../component-test/server";
// The pages this file renders (see component-test/routes.ts).
import "../views/OverviewView.vue";
import "../views/SettingsView.vue";

// From overview.spec.ts. Its own file: it counts MCP initializes, and the
// MCP session is module state, so it needs a page that has not opened one.

const AGENTS = [
	{
		id: "a1",
		name: "assistant",
		model: "anthropic/claude-sonnet-4-5",
		fallbacks: [],
		state: "idle",
	},
	{
		id: "a2",
		name: "coder",
		model: "openai/gpt-4o",
		fallbacks: [],
		state: "running",
	},
];

const JOBS = [
	{
		id: "j1",
		task_id: "nightly",
		agent_id: "assistant",
		status: "completed",
		attempts: 1,
		created_at: "",
		updated_at: "",
	},
	{
		id: "j2",
		task_id: "build",
		agent_id: "coder",
		status: "in_progress",
		attempts: 1,
		created_at: "",
		updated_at: "",
	},
];

// A failed first tool call is retried: a 404 means the session expired, so
// the client reinitializes first; a 502 is transient and retried as is.
test("recovers when the MCP session expires", async () => {
	mockMCP({
		agent_list: AGENTS,
		job_list: JOBS,
		config_validate: [],
	});
	let initializes = 0;
	let firstToolCallFails = true;
	route("/mcp", (r) => {
		const method = r.request().postDataJSON()?.method;
		if (method === "initialize") initializes += 1;
		if (method === "tools/call" && firstToolCallFails) {
			firstToolCallFails = false;
			return r.fulfill({ status: 404, body: "session not found" });
		}
		return r.fallback();
	});

	await renderApp("/system/overview");

	await expect.element(page.getByText("2").first()).toBeVisible();
	await expect.element(page.getByText("No issues found")).toBeVisible();
	expect(initializes).toBe(2);
});
