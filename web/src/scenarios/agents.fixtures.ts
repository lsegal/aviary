// Shared fixtures for the agents-*.component.test.ts files, ported from
// web/e2e/agents.spec.ts (#1636).
import { expect } from "vitest";
import type { Locator } from "vitest/browser";
import { mockMCP } from "../component-test/server";

export type Args = Record<string, unknown>;

export const CONFIG = {
	server: { port: 16677, tls: { cert: "", key: "" } },
	agents: [
		{
			name: "assistant",
			model: "anthropic/claude-sonnet-4-5",
			memory: "",
			fallbacks: [],
			channels: [
				{
					type: "signal",
					id: "+15551234567",
					primary: "+15551234567",
					disabled_tools: ["task_run"],
					allow_from: [
						{
							from: "*",
							respond_to_mentions: true,
							restrict_tools: ["task_run", "browser_open", "usage_query"],
						},
					],
				},
			],
			tasks: [
				{
					enabled: false,
					name: "daily-briefing",
					schedule: "0 * * * * *",
					prompt: "Summarize updates",
					target: "channel:+15551234567",
				},
			],
			permissions: {
				preset: "minimal",
				tools: ["task_run", "auth_set", "browser_open"],
			},
		},
	],
	models: {
		providers: {
			anthropic: { auth: "auth:anthropic:default" },
			openai: { auth: "auth:openai:default" },
		},
		defaults: { model: "anthropic/claude-sonnet-4-5", fallbacks: [] },
	},
	browser: { binary: "", cdp_port: 9222, reuse_tabs: true },
	search: { web: { brave_api_key: "auth:brave_api_key" } },
	scheduler: { concurrency: "auto" },
};

export const ASSISTANT = {
	id: "a1",
	name: "assistant",
	model: "anthropic/claude-sonnet-4-5",
	fallbacks: [],
	state: "idle",
};

export const TOOLS = [
	{ name: "task_run", description: "Run a task immediately" },
	{ name: "auth_set", description: "Store a secret" },
	{ name: "browser_open", description: "Open a browser tab" },
	{ name: "usage_query", description: "Read usage metrics" },
];

/** The agent root files store the e2e beforeEach mocked. */
export function agentFileFixtures() {
	const agentFiles = new Map<string, string>([
		["AGENTS.md", "# Agents"],
		["RULES.md", "# Rules"],
		["MEMORY.md", "Remembered note"],
		["IDENTITY.md", "# Identity"],
	]);
	return {
		agent_file_list: () => Array.from(agentFiles.keys()).sort(),
		agent_file_read: (args?: Args) =>
			agentFiles.get(String(args?.file ?? "")) ?? "",
		agent_file_write: (args?: Args) => {
			agentFiles.set(String(args?.file ?? ""), String(args?.content ?? ""));
			return "ok";
		},
		agent_file_delete: (args?: Args) => {
			agentFiles.delete(String(args?.file ?? ""));
			return "ok";
		},
	};
}

/** The e2e spec's beforeEach mock: one agent with root files. */
export function mockDefault() {
	mockMCP({
		config_get: CONFIG,
		auth_list: ["anthropic:default", "brave_api_key"],
		session_list: [],
		agent_list: [ASSISTANT],
		tool_list: TOOLS,
		...agentFileFixtures(),
	});
}

/** Each matched element's text, whitespace collapsed, as Playwright's
 * toHaveText with an array compares them. */
export function texts(loc: Locator): string[] {
	return loc
		.elements()
		.map((el) => (el.textContent ?? "").replace(/\s+/g, " ").trim());
}

export function count(loc: Locator): number {
	return loc.elements().length;
}

/** Playwright's toBeHidden: absent, or present but not visible. */
export async function expectHidden(loc: Locator) {
	await expect
		.poll(() => loc.elements().filter((el) => el.checkVisibility()).length)
		.toBe(0);
}
