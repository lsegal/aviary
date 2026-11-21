import { test } from "@playwright/test";
import { mockMCP, setAuthToken } from "/private/var/folders/_3/6nm7yxhj0v3716vn7dyl134h0000gn/T/aviary-gh-fix-421/web/e2e/helpers/mockMCP";
const CONFIG = {
	agents: [
		{
			name: "assistant", model: "anthropic/claude-sonnet-4-5", fallbacks: [], working_dir: "/home/user/projects",
			channels: [{ type: "signal", id: "+15551234567", primary: "+15551234567", allow_from: [{ from: "*", respond_to_mentions: true }] }],
			tasks: [], permissions: { preset: "standard" },
		},
	],
	models: { providers: { anthropic: { auth: "auth:anthropic:default" } }, defaults: { model: "anthropic/claude-sonnet-4-5", fallbacks: [] } },
};
test("shots", async ({ page }) => {
	await page.setViewportSize({ width: 1400, height: 900 });
	await setAuthToken(page);
	await mockMCP(page, { config_get: CONFIG, auth_list: ["anthropic:default"], session_list: [], job_list: [], config_validate: [],
		agent_list: [{ id: "a1", name: "assistant", model: "anthropic/claude-sonnet-4-5", fallbacks: [], state: "idle" }],
		tool_list: [{ name: "task_run", description: "Run a task" }], agent_file_list: [] });
	await page.goto("/agents/assistant/settings");
	await page.waitForTimeout(1500);
	await page.screenshot({ path: process.env.OUT + "-general.png" });
	await page.screenshot({ path: process.env.OUT + "-full.png", fullPage: true });
	await page.goto("/agents/assistant/settings/channels");
	await page.waitForTimeout(1500);
	await page.screenshot({ path: process.env.OUT + "-channels.png" });
});
