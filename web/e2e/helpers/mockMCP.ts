import type { Page } from "@playwright/test";

/** Fixture data keyed by MCP tool name. */
export interface ToolFixtures {
	agent_list?: object[];
	job_list?: object[] | null;
	config_validate?: object[];
	session_list?: object[];
	server_status?: object;
	task_run?: object | null;
	[key: string]: unknown;
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
			const data = toolName in fixtures ? fixtures[toolName] : [];
			return route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					jsonrpc: "2.0",
					id: body.id,
					result: { content: [{ type: "text", text: JSON.stringify(data) }] },
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
