import { beforeEach, expect, test } from "vitest";
import { page } from "vitest/browser";
import { goto, reload, renderApp } from "../component-test/app";
import { mockMCP, route } from "../component-test/server";
import { useNavPrefs } from "../composables/useNavPrefs";
import { normalizeNavPrefs } from "../lib/navPrefs";
// The pages this file renders (see component-test/routes.ts).
import "../views/ChannelView.vue";
import "../views/SettingsView.vue";

// From page-breadcrumb.spec.ts. Its own file: the navigation preferences
// are fetched once per page load (module state), so the first fetch must be
// this test's.

const LONG_PROJECT = "lsegal/a-repository-with-a-very-long-name-indeed";

/** Mocks agent "Aviary" with channel "signal", a task and two projects. */
function mockAviary(sessions: object[] = []) {
	mockMCP({
		agent_list: [{ id: "Aviary", name: "Aviary", model: "x/y", state: "idle" }],
		config_get: {
			agents: [
				{
					name: "Aviary",
					model: "x/y",
					channels: [{ type: "signal", id: "signal", deliver_to: "+1" }],
					tasks: [{ name: "release-aviary", every: "1h", prompt: "release" }],
					projects: [{ target: "o/r" }, { target: LONG_PROJECT }],
				},
			],
		},
		config_validate: [],
		auth_list: [],
		tool_list: [],
		job_list: [],
		job_query: [],
		task_list: [],
		session_list: sessions,
		session_messages: [],
		channel_sessions: [],
		project_status: {
			states: { starting: 0, paused: 0, running: 0, done: 0, failed: 0 },
			targets: ["o/r", LONG_PROJECT],
		},
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
	});
}

function breadcrumb() {
	return page.getByTestId("settings-breadcrumb");
}

beforeEach(() => {
	mockAviary();
});

test("the agent crumb opens the first page the user's sidebar shows", async () => {
	route("/api/preferences/navigation", (r) =>
		r.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				order: {
					agentPages: [
						"tasks",
						"chat",
						"projects",
						"channels",
						"files",
						"settings",
					],
				},
				hidden: { agentPages: ["tasks"] },
			}),
		}),
	);
	await renderApp("/agents/Aviary/channels/signal");
	// Scheduled Tasks comes first but is hidden, so Chats is the first shown.
	await expect
		.element(breadcrumb().getByRole("link", { name: "Aviary" }))
		.toHaveAttribute("href", "/agents/Aviary/chat");
	route("/api/preferences/navigation", (r) =>
		r.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				order: { agentPages: ["projects", "chat"] },
				hidden: {},
			}),
		}),
	);
	// A reload fetches the preferences again, but here the page's module
	// state survives it and the preferences were already fetched once:
	// apply the server's copy as a fresh page's first fetch does.
	await reload();
	const remote = await (await fetch("/api/preferences/navigation")).json();
	useNavPrefs().prefs.value = normalizeNavPrefs(remote);
	await expect
		.element(breadcrumb().getByRole("link", { name: "Aviary" }))
		.toHaveAttribute("href", "/agents/Aviary/projects");
	// Settings pages' agent crumb and back link follow the same order (#1091).
	for (const url of ["/agents/Aviary/settings", "/agents/Aviary/tasks"]) {
		await goto(url);
		await expect
			.element(breadcrumb().getByRole("link", { name: "Aviary" }))
			.toHaveAttribute("href", "/agents/Aviary/projects");
		await expect
			.element(page.getByTestId("settings-back"))
			.toHaveAttribute("href", "/agents/Aviary/projects");
	}
});
