import type { Page } from "@playwright/test";
import { mockMCP, setAuthToken, type ToolFixtures } from "./mockMCP";

// The routed pages that share the page header (#1147), and the agent their
// specs mock, for page-header.spec.ts and page-header-actions.spec.ts.

/** Mocks agent "Aviary" with a channel, a task, a file and a project. */
export async function mockAviary(page: Page, extra: ToolFixtures = {}) {
	await setAuthToken(page);
	await mockMCP(page, {
		agent_list: [{ id: "Aviary", name: "Aviary", model: "x/y", state: "idle" }],
		config_get: {
			agents: [
				{
					name: "Aviary",
					model: "x/y",
					channels: [{ type: "signal", id: "signal", deliver_to: "+1" }],
					tasks: [{ name: "release-aviary", every: "1h", prompt: "release" }],
					projects: [{ target: "o/r" }],
				},
			],
		},
		config_validate: [],
		auth_list: [],
		tool_list: [],
		job_list: [],
		job_query: [],
		task_list: [],
		session_list: [],
		session_messages: [],
		channel_sessions: [],
		agent_file_list: ["AGENTS.md"],
		agent_file_read: "Agent rules",
		project_status: {
			states: { starting: 0, paused: 0, running: 0, done: 0, failed: 0 },
			targets: ["o/r"],
		},
		project_job_list: { jobs: [], total: 0, offset: 0, limit: 50 },
		...extra,
	});
}

export const A = "/agents/Aviary";

/**
 * Every routed page, with its title, whether it has a parent and whether it
 * is a Code Loop board with the compact header (#1570).
 */
export const PAGES: Array<{
	url: string;
	title: string;
	back?: false;
	compact?: true;
}> = [
	{ url: "/system/overview", title: "Overview", back: false },
	{ url: "/system/usage", title: "Usage" },
	{ url: "/system/tools", title: "Tools" },
	{ url: "/system/skills", title: "Skills" },
	{ url: "/system/sessions", title: "Sessions" },
	{ url: "/system/models", title: "Models" },
	{ url: "/system/logs", title: "Logs" },
	{ url: "/system/daemons", title: "Daemons" },
	{ url: "/system/settings", title: "Settings" },
	{ url: "/system/providers", title: "Settings" },
	{ url: "/system/display", title: "Settings" },
	{ url: "/system/watches/w%2Fx", title: "Watch: w/x", compact: true },
	{ url: "/system/watches/w%2Fx/jobs/5", title: "#5" },
	{ url: "/projects/settings", title: "Settings" },
	{ url: `${A}/chat`, title: "Chats", back: false },
	{ url: `${A}/projects`, title: "Code Loop", compact: true },
	{ url: `${A}/projects/o%2Fr`, title: "o/r", compact: true },
	{ url: `${A}/projects/o%2Fr/jobs/1013`, title: "#1013" },
	{ url: `${A}/channels`, title: "Channels" },
	{ url: `${A}/channels/signal`, title: "signal" },
	{ url: `${A}/channels/signal/settings`, title: "Settings" },
	{ url: `${A}/tasks`, title: "Scheduled Tasks" },
	{ url: `${A}/tasks?task=release-aviary`, title: "Settings" },
	{ url: `${A}/tasks/release-aviary`, title: "release-aviary" },
	{ url: `${A}/tasks/release-aviary/runs/job_1`, title: "run job_1" },
	{ url: `${A}/files?file=AGENTS.md`, title: "AGENTS.md" },
	{ url: `${A}/settings`, title: "Agent Settings" },
	{ url: `${A}/settings/permissions`, title: "Permissions" },
];
