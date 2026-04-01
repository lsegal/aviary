import type { Page } from "@playwright/test";

/** Common argument shapes for MCP tool handlers used in tests. */
export interface AgentFileArgs {
	agent?: string;
	file?: string;
	content?: string;
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

/** Fixture data keyed by MCP tool name. */
export interface ToolFixtures {
	agent_list?: object[] | string;
	job_list?: object[] | null;
	config_validate?: object[];
	session_list?: object[] | string;
	server_status?: object;
	task_run?: ToolHandler<TaskRunArgs> | object | null;
	tool_list?: object[] | string;

	agent_file_list?: ToolHandler<AgentFileArgs, string[]> | string[];
	agent_file_read?: ToolHandler<AgentFileArgs> | string;
	agent_file_write?: ToolHandler<AgentFileArgs> | string;
	agent_file_delete?: ToolHandler<AgentFileArgs> | string;

	config_save?: ToolHandler<ConfigSaveArgs> | object;

	[key: string]:
		| unknown
		| ToolHandler<Record<string, unknown>, unknown>
		| ((args?: Record<string, unknown>) => unknown | Promise<unknown>);
}

/**
 * Intercepts /api/login and /mcp so tests run without a real server.
 * Call before page.goto(). MCP tool responses come from `fixtures`.
 */
export async function mockMCP(page: Page, fixtures: ToolFixtures = {}) {
	// Auth: always accept any token.
	await page.route("/api/login", (route) =>
		route.fulfill({ status: 200, contentType: "application/json", body: "{}" }),
	);

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
			const data =
				typeof fixture === "function"
					? await fixture(body.params?.arguments)
					: fixture;
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
export function setAuthToken(page: Page, token = "test-token") {
	return page.addInitScript(
		(t) => localStorage.setItem("aviary_token", t),
		token,
	);
}
