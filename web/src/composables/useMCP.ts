import { useRouter } from "vue-router";
import { useAuthStore } from "../stores/auth";

export interface MCPResult {
	content?: Array<{ type: string; text?: string }>;
	isError?: boolean;
	[key: string]: unknown;
}

type JsonRpcResponse = {
	id?: number;
	result?: MCPResult;
	error?: { message: string };
	method?: string;
	params?: Record<string, unknown>;
};

class MCPHTTPError extends Error {
	status: number;

	constructor(message: string, status: number) {
		super(message);
		this.name = "MCPHTTPError";
		this.status = status;
	}
}

interface CallToolOptions {
	onProgress?: (chunk: string) => void;
}

export interface MCPToolInfo {
	name: string;
	description?: string;
}

// Module-level session state — one session shared across all useMCP() calls.
let sessionId: string | null = null;
let initPromise: Promise<void> | null = null;

export function useMCP() {
	const auth = useAuthStore();
	const router = useRouter();

	function authHeaders(): Record<string, string> {
		const tok = auth.getToken();
		return tok ? { Authorization: `Bearer ${tok}` } : {};
	}

	function sessionHeaders(): Record<string, string> {
		return sessionId ? { "Mcp-Session-Id": sessionId } : {};
	}

	function resetSession() {
		sessionId = null;
		initPromise = null;
	}

	function isRecoverableSessionError(error: unknown): error is MCPHTTPError {
		return error instanceof MCPHTTPError && error.status === 404;
	}

	function httpError(prefix: string, res: Response): MCPHTTPError {
		return new MCPHTTPError(
			`${prefix}: ${res.status} ${res.statusText}`,
			res.status,
		);
	}

	async function post(body: unknown): Promise<Response> {
		const res = await fetch("/mcp", {
			method: "POST",
			headers: {
				"Content-Type": "application/json",
				Accept: "application/json, text/event-stream",
				...authHeaders(),
				...sessionHeaders(),
			},
			body: JSON.stringify(body),
		});
		if (res.status === 401) {
			auth.logout();
			router.push("/login");
			throw new Error("Unauthorized");
		}
		return res;
	}

	async function withSessionRetry<T>(op: () => Promise<T>): Promise<T> {
		try {
			return await op();
		} catch (error) {
			if (!isRecoverableSessionError(error)) throw error;
			resetSession();
			await ensureInitialized();
			return op();
		}
	}

	async function readResponse(
		res: Response,
		onEvent?: (evt: JsonRpcResponse) => void,
	): Promise<JsonRpcResponse> {
		const ct = res.headers.get("Content-Type") ?? "";
		if (ct.includes("text/event-stream")) {
			const reader = res.body?.getReader();
			if (!reader) throw new Error("No response body");

			const decoder = new TextDecoder();
			let buffer = "";
			let eventData: string[] = [];
			let finalMessage: JsonRpcResponse | null = null;

			const processLine = (line: string) => {
				const trimmed = line.endsWith("\r") ? line.slice(0, -1) : line;
				if (trimmed === "") {
					if (eventData.length === 0) return;
					const payload = eventData.join("\n").trim();
					eventData = [];
					if (!payload) return;
					try {
						const parsed = JSON.parse(payload) as JsonRpcResponse;
						onEvent?.(parsed);
						if (parsed.id !== undefined || parsed.result || parsed.error) {
							finalMessage = parsed;
						}
					} catch {
						// Ignore non-JSON events.
					}
					return;
				}
				if (trimmed.startsWith("data:")) {
					eventData.push(trimmed.slice(5).trimStart());
				}
			};

			for (;;) {
				const { done, value } = await reader.read();
				if (done) break;
				buffer += decoder.decode(value, { stream: true });
				let idx = buffer.indexOf("\n");
				for (; idx !== -1; idx = buffer.indexOf("\n")) {
					const line = buffer.slice(0, idx);
					buffer = buffer.slice(idx + 1);
					processLine(line);
				}
				if (finalMessage) {
					reader.cancel().catch(() => {});
					break;
				}
			}

			buffer += decoder.decode();
			if (buffer.length > 0) processLine(buffer);
			processLine("");

			if (!finalMessage) throw new Error("No data in SSE response");
			return finalMessage;
		}
		return res.json() as Promise<JsonRpcResponse>;
	}

	async function initialize(): Promise<void> {
		const res = await post({
			jsonrpc: "2.0",
			id: 1,
			method: "initialize",
			params: {
				protocolVersion: "2024-11-05",
				capabilities: {},
				clientInfo: { name: "aviary-web", version: "0.1.0" },
			},
		});
		if (!res.ok) throw httpError("MCP init failed", res);

		// Capture session ID if the server issued one.
		const sid = res.headers.get("Mcp-Session-Id");
		if (sid) sessionId = sid;

		await readResponse(res);

		// Send initialized notification (no response expected).
		const notifyRes = await post({
			jsonrpc: "2.0",
			method: "notifications/initialized",
			params: {},
		});
		if (!notifyRes.ok)
			throw httpError("MCP initialized notification failed", notifyRes);
		// Drain the response body if present so the browser does not keep the
		// request hanging across subsequent SPA navigations.
		await notifyRes.text().catch(() => "");
	}

	async function ensureInitialized(): Promise<void> {
		if (!initPromise) {
			initPromise = initialize().catch((e) => {
				initPromise = null;
				sessionId = null;
				throw e;
			});
		}
		return initPromise;
	}

	async function callTool(
		name: string,
		args?: Record<string, unknown>,
		options?: CallToolOptions,
	): Promise<string> {
		return withSessionRetry(async () => {
			await ensureInitialized();
			const progressToken =
				options?.onProgress && typeof crypto?.randomUUID === "function"
					? crypto.randomUUID()
					: options?.onProgress
						? `${Date.now()}-${Math.random().toString(36).slice(2)}`
						: undefined;

			const res = await post({
				jsonrpc: "2.0",
				id: Date.now(),
				method: "tools/call",
				params: {
					name,
					arguments: args ?? {},
					...(progressToken ? { _meta: { progressToken } } : {}),
				},
			});

			if (!res.ok) throw httpError("MCP error", res);

			const data = await readResponse(res, (evt) => {
				if (evt.method !== "notifications/progress") return;
				const msg = evt.params?.message;
				if (typeof msg === "string" && msg.length > 0) {
					options?.onProgress?.(msg);
				}
			});
			if (data.error) throw new Error(data.error.message);

			const content = data.result?.content ?? [];
			const text = content
				.filter((c) => c.type === "text")
				.map((c) => c.text ?? "")
				.join("");
			if (data.result?.isError) throw new Error(text || "tool call failed");
			return text;
		});
	}

	async function listTools(): Promise<MCPToolInfo[]> {
		return withSessionRetry(async () => {
			await ensureInitialized();
			const res = await post({
				jsonrpc: "2.0",
				id: Date.now(),
				method: "tools/list",
				params: {},
			});
			if (!res.ok) throw httpError("MCP error", res);
			const data = await readResponse(res);
			if (data.error) throw new Error(data.error.message);
			return (data.result?.tools as MCPToolInfo[] | undefined) ?? [];
		});
	}

	async function callToolWithRetry(
		name: string,
		args?: Record<string, unknown>,
		options?: CallToolOptions,
	): Promise<string> {
		return withSessionRetry(() => callTool(name, args, options));
	}

	return { callTool: callToolWithRetry, listTools };
}
