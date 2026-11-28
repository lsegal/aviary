import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { currentURL, renderApp } from "../component-test/app";
import { mockMCP, route } from "../component-test/server";

// From settings-header.spec.ts. Its own file: the navigation preferences
// are fetched once per page load (module state), so the first fetch must be
// this test's.

const NOW = new Date().toISOString();

/** Mocks agent "Aviary" with channel "signal", task "release-aviary" and project o/r. */
function mockSettings() {
	const config = {
		agents: [
			{
				name: "Aviary",
				model: "x/y",
				channels: [{ type: "signal", id: "signal", deliver_to: "+1" }],
				tasks: [{ name: "release-aviary", every: "1h", prompt: "release" }],
				projects: [{ target: "o/r" }],
			},
		],
	};
	const saves: unknown[] = [];
	mockMCP({
		agent_list: [{ id: "Aviary", name: "Aviary", model: "x/y", state: "idle" }],
		config_get: () => config,
		config_save: (args?: Record<string, unknown>) => {
			saves.push(args);
			Object.assign(config, JSON.parse(String(args?.config ?? "{}")));
			return "ok";
		},
		config_validate: [],
		auth_list: [],
		tool_list: [],
		job_list: [],
		job_query: [],
		session_list: [
			{ id: "main", agent_id: "Aviary", name: "main", created_at: NOW },
		],
		session_messages: [],
		channel_sessions: [
			{
				id: "channel:signal:signal",
				agent_id: "Aviary",
				name: "channel:signal:signal",
				created_at: NOW,
				own: true,
			},
		],
	});
	return { saves };
}

function breadcrumb() {
	return page.getByRole("navigation", { name: "Breadcrumb" });
}

function backLink() {
	return page.getByTestId("settings-back");
}

test("settings pages' agent crumb opens the first page the user's sidebar shows", async () => {
	mockSettings();
	route("/api/preferences/navigation", (r) =>
		r.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				order: { agentPages: ["chat", "projects", "channels"] },
				hidden: { agentPages: ["chat"] },
			}),
		}),
	);
	// Chats is hidden, so the agent's first page is Code Loop, as on the
	// task page (#1091).
	const home = "/agents/Aviary/projects";
	for (const url of [
		"/agents/Aviary/tasks/release-aviary",
		"/agents/Aviary/tasks?task=release-aviary",
		"/agents/Aviary/tasks",
		"/agents/Aviary/channels/signal/settings",
		"/agents/Aviary/settings/permissions",
		"/agents/Aviary/settings",
	]) {
		await renderApp(url);
		await expect
			.element(breadcrumb().getByRole("link", { name: "Aviary", exact: true }))
			.toHaveAttribute("href", home);
	}
	await expect.element(backLink()).toHaveAccessibleName("Back to Aviary");
	await backLink().click();
	await expect.poll(currentURL).toBe(home);
});
