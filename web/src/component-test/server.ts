// The fake Aviary server component tests run against (#1636). It replaces
// fetch, WebSocket and EventSource in the test page, so the app's requests
// never leave the browser. Its API follows Playwright's page.route,
// page.routeWebSocket and the e2e mockMCP helper (web/e2e/helpers), so a
// scenario reads the same in a component test as it did in a spec.

import toolArguments from "../../e2e/helpers/toolArguments.json" with {
	type: "json",
};

export type ToolHandler<P = Record<string, unknown>, R = unknown> = (
	args?: P,
) => R | Promise<R>;

export interface MCPStreamFixture {
	stream: string[];
	result?: unknown;
}

/** Fixture data keyed by MCP tool name: a value, or a handler of the call's
 * arguments. A handler that throws fails the tool call. */
export type ToolFixtures = Record<string, unknown>;

/** The request a route handler sees, as Playwright's Request. */
export interface RouteRequest {
	url(): string;
	method(): string;
	headers(): Record<string, string>;
	postData(): string | null;
	// biome-ignore lint/suspicious/noExplicitAny: as Playwright types it, so handlers read fields directly.
	postDataJSON(): any;
}

export interface FulfillOptions {
	status?: number;
	contentType?: string;
	headers?: Record<string, string>;
	body?: string;
	json?: unknown;
}

/** A routed request, as Playwright's Route. */
export interface Route {
	request(): RouteRequest;
	fulfill(options?: FulfillOptions): Promise<void>;
	abort(): Promise<void>;
	/** Passes the request to the next matching route (the defaults last). */
	fallback(): Promise<void>;
	/** The same as fallback(): there is no real server to continue to. */
	continue(): Promise<void>;
}

export type RouteHandler = (route: Route) => unknown | Promise<unknown>;
export type URLMatcher = string | RegExp | ((url: URL) => boolean);

interface RouteEntry {
	matcher: URLMatcher;
	handler: RouteHandler;
	times?: number;
}

type Outcome =
	| { kind: "fulfill"; options: FulfillOptions }
	| { kind: "abort" }
	| { kind: "fallback" };

const realFetch = window.fetch.bind(window);
const RealWebSocket = window.WebSocket;
const RealEventSource = window.EventSource;

let routes: RouteEntry[] = [];
let wsRoutes: { matcher: URLMatcher; handler: WebSocketHandler }[] = [];
let sockets: FakeWebSocket[] = [];
let eventSources: FakeEventSource[] = [];
let requests: { url: string; method: string; body: string | null }[] = [];

function matches(matcher: URLMatcher, url: URL): boolean {
	if (typeof matcher === "function") return matcher(url);
	if (matcher instanceof RegExp) return matcher.test(url.href);
	if (matcher.includes("*")) {
		// A Playwright-style glob: ** spans path segments, * stays in one.
		const re = new RegExp(
			`^${matcher
				.replace(/[.+?^${}()|[\]\\]/g, "\\$&")
				.replace(/\*\*/g, "__GLOBSTAR__")
				.replace(/\*/g, "[^/]*")
				.replace(/__GLOBSTAR__/g, ".*")}$`,
		);
		return re.test(matcher.startsWith("/") ? url.pathname : url.href);
	}
	if (matcher.startsWith("/")) return url.pathname === matcher;
	return url.href === matcher;
}

/** Routes requests whose URL matches to `handler`, ahead of every route
 * added before it, as page.route does. A string starting with `/` matches
 * the path; a RegExp matches the whole URL. */
export function route(
	matcher: URLMatcher,
	handler: RouteHandler,
	options: { times?: number } = {},
): void {
	routes.unshift({ matcher, handler, times: options.times });
}

/** Removes the routes added for `matcher`, or only `handler`'s. */
export function unroute(matcher: URLMatcher, handler?: RouteHandler): void {
	routes = routes.filter(
		(r) =>
			String(r.matcher) !== String(matcher) ||
			(handler !== undefined && r.handler !== handler),
	);
}

/** Every request the app sent since the test started. */
export function requestLog(): readonly {
	url: string;
	method: string;
	body: string | null;
}[] {
	return requests;
}

function makeRequest(
	url: URL,
	method: string,
	headers: Headers,
	body: string | null,
): RouteRequest {
	return {
		url: () => url.href,
		method: () => method,
		headers: () => Object.fromEntries(headers.entries()),
		postData: () => body,
		postDataJSON: () => (body === null ? null : JSON.parse(body)),
	};
}

async function dispatch(request: RouteRequest, url: URL): Promise<Outcome> {
	for (const entry of [...routes]) {
		if (!matches(entry.matcher, url)) continue;
		if (entry.times !== undefined) {
			entry.times -= 1;
			if (entry.times <= 0) routes = routes.filter((r) => r !== entry);
		}
		let outcome: Outcome | undefined;
		const settle = (o: Outcome) => {
			outcome ??= o;
			return Promise.resolve();
		};
		await entry.handler({
			request: () => request,
			fulfill: (options = {}) => settle({ kind: "fulfill", options }),
			abort: () => settle({ kind: "abort" }),
			fallback: () => settle({ kind: "fallback" }),
			continue: () => settle({ kind: "fallback" }),
		});
		if (!outcome || outcome.kind === "fallback") continue;
		return outcome;
	}
	return { kind: "fallback" };
}

function toResponse(options: FulfillOptions): Response {
	const headers = new Headers(options.headers);
	let body = options.body ?? "";
	if (options.json !== undefined) {
		body = JSON.stringify(options.json);
		if (!headers.has("content-type"))
			headers.set("content-type", "application/json");
	}
	if (options.contentType) headers.set("content-type", options.contentType);
	const status = options.status ?? 200;
	return new Response(status === 204 || status === 304 ? null : body, {
		status,
		headers,
	});
}

async function fakeFetch(
	input: RequestInfo | URL,
	init?: RequestInit,
): Promise<Response> {
	const req = new Request(input, init);
	const url = new URL(req.url, location.href);
	// Vite's own module requests keep going to the dev server.
	if (url.origin !== location.origin || isViteRequest(url)) {
		return realFetch(input, init);
	}
	const body =
		req.method === "GET" || req.method === "HEAD" ? null : await req.text();
	requests.push({ url: url.href, method: req.method, body });
	const outcome = await dispatch(
		makeRequest(url, req.method, req.headers, body),
		url,
	);
	if (outcome.kind === "abort") throw new TypeError("Failed to fetch");
	if (outcome.kind === "fulfill") return toResponse(outcome.options);
	return new Response(JSON.stringify({ error: "not found" }), {
		status: 404,
		headers: { "content-type": "application/json" },
	});
}

function isViteRequest(url: URL): boolean {
	return !(url.pathname.startsWith("/api/") || url.pathname === "/mcp");
}

// ---- WebSocket --------------------------------------------------------

/** The server side of a routed WebSocket, as Playwright's WebSocketRoute. */
export interface WebSocketRoute {
	url(): string;
	send(message: string): void;
	onMessage(handler: (message: string) => void): void;
	close(options?: { code?: number; reason?: string }): void;
}

export type WebSocketHandler = (ws: WebSocketRoute) => unknown;

/** Routes WebSockets whose URL matches to `handler`, as
 * page.routeWebSocket does. Unrouted sockets open and stay silent. */
export function routeWebSocket(
	matcher: URLMatcher,
	handler: WebSocketHandler,
): void {
	wsRoutes.unshift({ matcher, handler });
}

/** The WebSockets the app opened, newest last. */
export function webSockets(): readonly FakeWebSocket[] {
	return sockets;
}

class FakeWebSocket extends EventTarget {
	static readonly CONNECTING = 0;
	static readonly OPEN = 1;
	static readonly CLOSING = 2;
	static readonly CLOSED = 3;
	readonly CONNECTING = 0;
	readonly OPEN = 1;
	readonly CLOSING = 2;
	readonly CLOSED = 3;
	readonly url: string;
	readyState = 0;
	protocol = "";
	extensions = "";
	binaryType: BinaryType = "blob";
	bufferedAmount = 0;
	onopen: ((e: Event) => void) | null = null;
	onmessage: ((e: MessageEvent) => void) | null = null;
	onclose: ((e: CloseEvent) => void) | null = null;
	onerror: ((e: Event) => void) | null = null;
	private serverHandlers: ((message: string) => void)[] = [];

	constructor(url: string | URL) {
		super();
		this.url = String(url);
		sockets.push(this);
		const parsed = new URL(this.url, location.href);
		const entry = wsRoutes.find((r) => matches(r.matcher, parsed));
		const server: WebSocketRoute = {
			url: () => this.url,
			send: (message) => this.receive(message),
			onMessage: (h) => this.serverHandlers.push(h),
			close: (options) => this.serverClose(options?.code, options?.reason),
		};
		setTimeout(() => {
			if (this.readyState !== 0) return;
			this.readyState = 1;
			this.emit(new Event("open"), this.onopen);
			entry?.handler(server);
		}, 0);
	}

	private emit<E extends Event>(
		event: E,
		handler: ((e: E) => void) | null,
	): void {
		handler?.call(this, event);
		this.dispatchEvent(event);
	}

	/** Delivers `message` to the app as if the server sent it. */
	receive(message: string): void {
		if (this.readyState !== 1) return;
		this.emit(new MessageEvent("message", { data: message }), this.onmessage);
	}

	send(data: string): void {
		if (this.readyState !== 1) return;
		for (const h of this.serverHandlers) h(String(data));
	}

	close(code = 1000, reason = ""): void {
		this.serverClose(code, reason);
	}

	private serverClose(code = 1000, reason = ""): void {
		if (this.readyState >= 2) return;
		this.readyState = 3;
		this.emit(
			new CloseEvent("close", { code, reason, wasClean: true }),
			this.onclose,
		);
	}
}

// ---- EventSource ------------------------------------------------------

/** The EventSources the app opened, newest last. Tests push events with
 * emit(). */
export function eventSourcesOpened(): readonly FakeEventSource[] {
	return eventSources;
}

class FakeEventSource extends EventTarget {
	static readonly CONNECTING = 0;
	static readonly OPEN = 1;
	static readonly CLOSED = 2;
	readonly url: string;
	readonly withCredentials = false;
	readyState = 0;
	onopen: ((e: Event) => void) | null = null;
	onmessage: ((e: MessageEvent) => void) | null = null;
	onerror: ((e: Event) => void) | null = null;

	constructor(url: string | URL) {
		super();
		this.url = new URL(String(url), location.href).href;
		eventSources.push(this);
		setTimeout(() => {
			if (this.readyState !== 0) return;
			this.readyState = 1;
			const e = new Event("open");
			this.onopen?.(e);
			this.dispatchEvent(e);
		}, 0);
	}

	/** Delivers an event to the app; `type` defaults to "message". */
	emit(data: string, type = "message"): void {
		if (this.readyState === 2) return;
		const e = new MessageEvent(type, { data });
		if (type === "message") this.onmessage?.(e);
		this.dispatchEvent(e);
	}

	close(): void {
		this.readyState = 2;
	}
}

// ---- MCP --------------------------------------------------------------

/** The arguments the real server's schema for `tool` does not declare,
 * which it rejects. toolArguments.json is kept current by
 * TestToolArgumentsFileIsCurrent in internal/mcp. */
export function undeclaredArguments(
	tool: string,
	args: Record<string, unknown> = {},
): string[] {
	const declared = (toolArguments as Record<string, string[]>)[tool];
	if (!declared) return [];
	return Object.keys(args).filter((name) => !declared.includes(name));
}

function isStream(value: unknown): value is MCPStreamFixture {
	return (
		typeof value === "object" &&
		value !== null &&
		Array.isArray((value as MCPStreamFixture).stream)
	);
}

function jsonRPC(id: unknown, result: unknown): FulfillOptions {
	return {
		status: 200,
		contentType: "application/json",
		body: JSON.stringify({ jsonrpc: "2.0", id, result }),
	};
}

/** Answers the app's sign-in, preferences, notifications and MCP requests
 * from `fixtures`, as the e2e mockMCP helper does. */
export function mockMCP(fixtures: ToolFixtures = {}): void {
	route("/api/health", (r) => r.fulfill({ json: {} }));
	route("/api/login", (r) => r.fulfill({ json: {} }));

	// Preferences are kept in memory, as the server keeps them on disk, so
	// they survive a reload().
	const prefs: Record<string, unknown> = {
		navigation: { order: {}, hidden: {} },
		display: {},
		notifications: {},
	};
	const empty = structuredClone(prefs);
	for (const name of Object.keys(prefs)) {
		route(`/api/preferences/${name}`, (r) => {
			const method = r.request().method();
			if (method === "PUT") prefs[name] = r.request().postDataJSON();
			if (method === "DELETE") prefs[name] = structuredClone(empty[name]);
			return r.fulfill({ json: prefs[name] });
		});
	}
	route(/\/api\/notifications(\/.*)?$/, (r) =>
		r.fulfill({ json: { notifications: [], unread: 0 } }),
	);

	route("/mcp", async (r) => {
		const body = r.request().postDataJSON() as {
			id?: number;
			method: string;
			params?: { name?: string; arguments?: Record<string, unknown> };
		};
		if (body.method === "initialize") {
			return r.fulfill({
				...jsonRPC(body.id, {
					protocolVersion: "2024-11-05",
					capabilities: {},
					serverInfo: { name: "aviary-mock", version: "0.0.0" },
				}),
				headers: { "Mcp-Session-Id": "mock-session" },
			});
		}
		if (body.method === "notifications/initialized") {
			return r.fulfill({ status: 200, body: "{}" });
		}
		if (body.method === "tools/list") {
			return r.fulfill(jsonRPC(body.id, { tools: fixtures.tool_list ?? [] }));
		}
		if (body.method !== "tools/call") {
			return r.fulfill({ status: 200, body: "{}" });
		}
		const tool = body.params?.name ?? "";
		const fixture = tool in fixtures ? fixtures[tool] : [];
		let data: unknown;
		try {
			const undeclared = undeclaredArguments(tool, body.params?.arguments);
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
			return r.fulfill(
				jsonRPC(body.id, {
					isError: true,
					content: [
						{ type: "text", text: e instanceof Error ? e.message : String(e) },
					],
				}),
			);
		}
		if (isStream(data)) {
			const events: unknown[] = data.stream.map((message) => ({
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
			return r.fulfill({
				status: 200,
				contentType: "text/event-stream",
				body: events.map((e) => `data: ${JSON.stringify(e)}\n\n`).join(""),
			});
		}
		const text = typeof data === "string" ? data : JSON.stringify(data);
		return r.fulfill(jsonRPC(body.id, { content: [{ type: "text", text }] }));
	});
}

/** Installs the fake server in the page. */
export function installServer(): void {
	window.fetch = fakeFetch as typeof fetch;
	window.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
	window.EventSource = FakeEventSource as unknown as typeof EventSource;
}

/** Forgets every route, socket and logged request. */
export function resetServer(): void {
	for (const s of sockets) s.close();
	for (const s of eventSources) s.close();
	routes = [];
	wsRoutes = [];
	sockets = [];
	eventSources = [];
	requests = [];
}

/** Restores the browser's own fetch, WebSocket and EventSource. */
export function uninstallServer(): void {
	window.fetch = realFetch;
	window.WebSocket = RealWebSocket;
	window.EventSource = RealEventSource;
}
