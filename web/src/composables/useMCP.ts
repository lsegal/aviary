import { useRouter } from "vue-router";
import { useAuthStore } from "../stores/auth";

export interface MCPResult {
	content?: Array<{ type: string; text?: string }>;
	isError?: boolean;
	[key: string]: unknown;
}

type JsonRpcID = string | number | null;

type JsonRpcResponse = {
	id?: JsonRpcID;
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
	agentId?: string;
}

export interface MCPToolInfo {
	name: string;
	description?: string;
	inputSchema?: {
		type?: string | string[];
		required?: string[];
		properties?: Record<string, Record<string, unknown>>;
		[key: string]: unknown;
	};
}

// Module-level session state — one session shared across all useMCP() calls.
let sessionId: string | null = null;
let initPromise: Promise<void> | null = null;
let nextRequestId = 2;
const RETRYABLE_HTTP_STATUSES = new Set([500, 502, 503, 504]);
const TRANSIENT_RETRY_DELAYS_MS = [250, 750, 1_500, 3_000];

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
		nextRequestId = 2;
	}

	function allocateRequestId(): number {
		const id = nextRequestId;
		nextRequestId += 1;
		return id;
	}

	function isRecoverableSessionError(error: unknown): error is MCPHTTPError {
		return error instanceof MCPHTTPError && error.status === 404;
	}

	function isTransientTransportError(error: unknown): boolean {
		if (
			error instanceof MCPHTTPError &&
			RETRYABLE_HTTP_STATUSES.has(error.status)
		) {
			return true;
		}
		return error instanceof TypeError;
	}

	function httpError(prefix: string, res: Response): MCPHTTPError {
		return new MCPHTTPError(
			`${prefix}: ${res.status} ${res.statusText}`,
			res.status,
		);
	}

	async function delay(ms: number): Promise<void> {
		await new Promise((resolve) => setTimeout(resolve, ms));
	}

	async function post(
		body: unknown,
		extraHeaders?: Record<string, string>,
	): Promise<Response> {
		const res = await fetch("/mcp", {
			method: "POST",
			headers: {
				"Content-Type": "application/json",
				Accept: "application/json, text/event-stream",
				...authHeaders(),
				...sessionHeaders(),
				...extraHeaders,
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

	async function withRetry<T>(op: () => Promise<T>): Promise<T> {
		let sessionRetried = false;
		for (let attempt = 0; ; attempt += 1) {
			try {
				return await op();
			} catch (error) {
				if (!sessionRetried && isRecoverableSessionError(error)) {
					sessionRetried = true;
					resetSession();
					await ensureInitialized();
					continue;
				}
				if (
					attempt < TRANSIENT_RETRY_DELAYS_MS.length &&
					isTransientTransportError(error)
				) {
					await delay(TRANSIENT_RETRY_DELAYS_MS[attempt]);
					continue;
				}
				throw error;
			}
		}
	}

	async function readResponse(
		res: Response,
		options?: {
			expectedId?: JsonRpcID;
			onEvent?: (evt: JsonRpcResponse) => void;
		},
	): Promise<JsonRpcResponse> {
		const ct = res.headers.get("Content-Type") ?? "";
		if (ct.includes("text/event-stream")) {
			const reader = res.body?.getReader();
			if (!reader) throw new Error("No response body");

			const decoder = new TextDecoder();
			let buffer = "";
			let eventData: string[] = [];
			let finalMessage: JsonRpcResponse | null = null;
			let terminalMessage: JsonRpcResponse | null = null;

			const processLine = (line: string) => {
				const trimmed = line.endsWith("\r") ? line.slice(0, -1) : line;
				if (trimmed === "") {
					if (eventData.length === 0) return;
					// Coalesce all `data:` lines for this event into a single payload
					const payload = eventData.join("\n").trim();
					eventData = [];
					if (!payload) return;
					try {
						// In development, emit one debug log for the fully-assembled SSE payload
						if (
							typeof import.meta !== "undefined" &&
							(import.meta as ImportMeta).env?.DEV
						) {
							console.debug("SSE payload:", payload);
						}
						const parsed = JSON.parse(payload) as JsonRpcResponse;
						options?.onEvent?.(parsed);
						const isTerminalMessage =
							parsed.result !== undefined || parsed.error !== undefined;
						if (isTerminalMessage) {
							terminalMessage = parsed;
						}
						if (options?.expectedId === undefined) {
							if (isTerminalMessage) {
								finalMessage = parsed;
							}
							return;
						}
						if (
							parsed.id !== undefined &&
							parsed.id !== null &&
							String(parsed.id) === String(options.expectedId)
						) {
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

			if (finalMessage) return finalMessage;
			if (terminalMessage) return terminalMessage;
			throw new Error("No data in SSE response");
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

		await readResponse(res, { expectedId: 1 });

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
		return withRetry(async () => {
			await ensureInitialized();
			const requestId = allocateRequestId();
			const progressToken =
				options?.onProgress && typeof crypto?.randomUUID === "function"
					? crypto.randomUUID()
					: options?.onProgress
						? `${requestId}-${Math.random().toString(36).slice(2)}`
						: undefined;

			const agentHeaders = options?.agentId
				? { "X-Aviary-Agent-ID": options.agentId }
				: undefined;
			const res = await post(
				{
					jsonrpc: "2.0",
					id: requestId,
					method: "tools/call",
					params: {
						name,
						arguments: args ?? {},
						...(progressToken ? { _meta: { progressToken } } : {}),
					},
				},
				agentHeaders,
			);

			if (!res.ok) throw httpError("MCP error", res);

			const data = await readResponse(res, {
				expectedId: requestId,
				onEvent: (evt) => {
					if (evt.method !== "notifications/progress") return;
					const msg = evt.params?.message;
					if (typeof msg === "string" && msg.length > 0) {
						options?.onProgress?.(msg);
					}
				},
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
		return withRetry(async () => {
			await ensureInitialized();
			const requestId = allocateRequestId();
			const res = await post({
				jsonrpc: "2.0",
				id: requestId,
				method: "tools/list",
				params: {},
			});
			if (!res.ok) throw httpError("MCP error", res);
			const data = await readResponse(res, { expectedId: requestId });
			if (data.error) throw new Error(data.error.message);
			return (data.result?.tools as MCPToolInfo[] | undefined) ?? [];
		});
	}

	return { callTool, listTools };
}
