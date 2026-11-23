import type { Page } from "@playwright/test";
import toolArguments from "./toolArguments.json" with { type: "json" };

/** Common argument shapes for MCP tool handlers used in tests. */
export interface AgentFileArgs {
	agent?: string;
	file?: string;
	content?: string;
	to?: string;
}

export interface ConfigSaveArgs {
	config?: string;
}

export interface TaskRunArgs {
	name: string;
	force?: boolean | string;
}

export type ToolHandler<P = Record<string, unknown>, R = unknown> = (
	args?: P,
) => R | Promise<R>;

export interface MCPStreamFixture {
	stream: string[];
	result?: unknown;
}

/** Fixture data keyed by MCP tool name. */
export interface ToolFixtures {
	agent_list?: ToolHandler | object[] | string;
	job_list?: ToolHandler | object[] | null;
	config_validate?: object[];
	session_list?: ToolHandler | object[] | string;
	server_status?: object;
	task_run?: ToolHandler<TaskRunArgs> | object | null;
	tool_list?: object[] | string;

	agent_file_list?: ToolHandler<AgentFileArgs, string[]> | string[];
	agent_file_read?: ToolHandler<AgentFileArgs> | string;
	agent_file_write?: ToolHandler<AgentFileArgs> | string;
	agent_file_delete?: ToolHandler<AgentFileArgs> | string;
	agent_file_rename?: ToolHandler<AgentFileArgs> | string;

	config_save?: ToolHandler<ConfigSaveArgs> | object;

	[key: string]:
		| unknown
		| MCPStreamFixture
		| ToolHandler<Record<string, unknown>, unknown>
		| ((args?: Record<string, unknown>) => unknown | Promise<unknown>);
}

function isMCPStreamFixture(value: unknown): value is MCPStreamFixture {
	return (
		typeof value === "object" &&
		value !== null &&
		Array.isArray((value as MCPStreamFixture).stream)
	);
}

/**
 * The arguments that tools/call passes and the real server's schema for that
 * tool does not declare, which the server rejects. toolArguments.json is kept
 * current by TestToolArgumentsFileIsCurrent in internal/mcp.
 */
export function undeclaredArguments(
	tool: string,
	args: Record<string, unknown> = {},
): string[] {
	const declared = (toolArguments as Record<string, string[]>)[tool];
	if (!declared) return [];
	return Object.keys(args).filter((name) => !declared.includes(name));
}

type MCPStreamEvent =
	| {
			jsonrpc: string;
			method: string;
			params: { message: string };
	  }
	| {
			jsonrpc: string;
			id: number | undefined;
			result: {
				content: Array<{ type: string; text: string }>;
			};
	  };

/**
 * Intercepts /api/login and /mcp so tests run without a real server.
 * Call before page.goto(). MCP tool responses come from `fixtures`.
 */
export async function mockMCP(page: Page, fixtures: ToolFixtures = {}) {
	// Auth: always accept any token.
	await page.route("/api/login", (route) =>
		route.fulfill({ status: 200, contentType: "application/json", body: "{}" }),
	);

	// Navigation preferences (#1003), kept in memory as the server keeps
	// them on disk, so they survive reloads. Specs can route the endpoint
	// again to fail it or inspect saves.
	let navPrefs = { order: {}, hidden: {} };
	await page.route("/api/preferences/navigation", async (route) => {
		const method = route.request().method();
		if (method === "PUT") navPrefs = route.request().postDataJSON();
		if (method === "DELETE") navPrefs = { order: {}, hidden: {} };
		await route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify(navPrefs),
		});
	});

	await page.route("/mcp", async (route) => {
		const body = route.request().postDataJSON() as {
			jsonrpc: string;
			id?: number;
			method: string;
			params?: { name?: string; arguments?: Record<string, unknown> };
		};

		// MCP initialize handshake.
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

		// Initialized notification — no response body needed.
		if (body.method === "notifications/initialized") {
			return route.fulfill({ status: 200, body: "{}" });
		}

		// Tool calls.
		if (body.method === "tools/call") {
			const toolName = body.params?.name ?? "";
			const fixture = toolName in fixtures ? fixtures[toolName] : [];
			let data: unknown;
			try {
				// The real server validates arguments before any handler runs.
				const undeclared = undeclaredArguments(
					toolName,
					body.params?.arguments,
				);
				if (undeclared.length) {
					throw new Error(
						`validating "arguments": validating root: unexpected additional properties ${JSON.stringify(undeclared)}`,
					);
				}
				data =
					typeof fixture === "function"
						? await fixture(body.params?.arguments)
						: fixture;
			} catch (e) {
				// A throwing fixture is a failed tool call, as the server
				// reports one.
				return route.fulfill({
					status: 200,
					contentType: "application/json",
					body: JSON.stringify({
						jsonrpc: "2.0",
						id: body.id,
						result: {
							isError: true,
							content: [
								{
									type: "text",
									text: e instanceof Error ? e.message : String(e),
								},
							],
						},
					}),
				});
			}
			if (isMCPStreamFixture(data)) {
				const events: MCPStreamEvent[] = data.stream.map((message) => ({
					jsonrpc: "2.0",
					method: "notifications/progress",
					params: { message },
				}));
				events.push({
					jsonrpc: "2.0",
					id: body.id,
					result: {
						content: [
							{
								type: "text",
								text:
									typeof data.result === "string"
										? data.result
										: JSON.stringify(data.result ?? ""),
							},
						],
					},
				});
				return route.fulfill({
					status: 200,
					contentType: "text/event-stream",
					body: events
						.map((event) => `data: ${JSON.stringify(event)}\n\n`)
						.join(""),
				});
			}
			const text = typeof data === "string" ? data : JSON.stringify(data);
			return route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: { content: [{ type: "text", text }] },
				}),
			});
		}

		if (body.method === "tools/list") {
			return route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: { tools: fixtures.tool_list ?? [] },
				}),
			});
		}

		// Fallback.
		return route.fulfill({ status: 200, body: "{}" });
	});
}

/** Sets the auth token in localStorage before page scripts run. */
/**
 * Signs the page in. Unless `onboarding` is set, the setup wizard counts as
 * skipped, so specs whose fixtures list no agents still reach their page.
 */
export function setAuthToken(
	page: Page,
	token = "test-token",
	{ onboarding = false } = {},
) {
	return page.addInitScript(
		({ t, onboarding }) => {
			localStorage.setItem("aviary_token", t);
			if (!onboarding) sessionStorage.setItem("aviary_onboarding_skipped", "1");
		},
		{ t: token, onboarding },
	);
}
