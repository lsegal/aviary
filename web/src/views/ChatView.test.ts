// @vitest-environment happy-dom
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import ChatComposer from "../components/chat/ChatComposer.vue";
import { chatDraftKey, getChatDraft } from "../components/chat/drafts";
import { useChatSessionsStore } from "../stores/chatSessions";
import ChatView from "./ChatView.vue";

// #1607: ChatView mounted against a fake MCP server, reply stream and
// session WebSocket.

const fake = vi.hoisted(() => ({
	callTool: vi.fn(),
	streamAgent: vi.fn(),
}));

vi.mock("../composables/useMCP", () => ({
	useMCP: () => ({ callTool: fake.callTool }),
}));
vi.mock("../composables/useStream", () => ({
	useStream: () => ({ streamAgent: fake.streamAgent }),
}));
vi.mock("../components/AppLayout.vue", async () => {
	const { defineComponent, h } = await import("vue");
	return {
		default: defineComponent({
			setup:
				(_, { slots }) =>
				() =>
					h("div", { "data-testid": "app-layout" }, slots.default?.()),
		}),
	};
});
// The chat picker is a Radix dropdown; plain elements keep its items
// rendered and clickable.
vi.mock("radix-vue", async (importOriginal) => {
	const { defineComponent, h } = await import("vue");
	const passthrough = (tag: string) =>
		defineComponent({
			setup:
				(_, { slots }) =>
				() =>
					h(tag, slots.default?.()),
		});
	return {
		...(await importOriginal<object>()),
		DropdownMenuRoot: passthrough("div"),
		DropdownMenuTrigger: passthrough("div"),
		DropdownMenuContent: passthrough("div"),
		DropdownMenuItem: defineComponent({
			emits: ["select"],
			setup:
				(_, { slots, emit }) =>
				() =>
					h(
						"div",
						{ "data-testid": "chat-menu-item", onClick: () => emit("select") },
						slots.default?.(),
					),
		}),
	};
});

interface FakeSession {
	id: string;
	agent_id: string;
	name: string;
	title?: string;
	created_at: string;
	is_processing?: boolean;
}

class FakeWebSocket {
	static instances: FakeWebSocket[] = [];
	onopen: (() => void) | null = null;
	onclose: (() => void) | null = null;
	onmessage: ((e: { data: string }) => Promise<void>) | null = null;
	closed = false;
	constructor(public url: string) {
		FakeWebSocket.instances.push(this);
	}
	close() {
		this.closed = true;
	}
	async emit(frame: unknown) {
		await this.onmessage?.({
			data: typeof frame === "string" ? frame : JSON.stringify(frame),
		});
		await settle();
	}
}

const DAY1 = "2026-01-05T10:00:00Z";
const DAY2 = "2026-01-06T10:00:00Z";

let server: {
	agents: { name: string; model?: string }[];
	sessions: Record<string, FakeSession[]>;
	messages: Record<string, unknown[]>;
	turns: Record<string, unknown>;
	fail: Set<string>;
};
let router: Router;
let wrapper: VueWrapper | undefined;

function session(id: string, extra: Partial<FakeSession> = {}): FakeSession {
	return { id, agent_id: "bot", name: id, created_at: DAY1, ...extra };
}

async function settle() {
	for (let i = 0; i < 6; i++) await flushPromises();
}

function calls(name: string) {
	return fake.callTool.mock.calls.filter(([n]) => n === name);
}

function ws() {
	const socket = FakeWebSocket.instances.at(-1);
	if (!socket) throw new Error("no WebSocket");
	return socket;
}

async function mountChat(
	props: Record<string, string> = { agent: "bot" },
	path = "/",
) {
	await router.push(path);
	wrapper = mount(ChatView, {
		props,
		attachTo: document.body,
		global: {
			plugins: [router],
			stubs: { ChatComposer: true, ChatToolCall: true },
		},
	});
	await settle();
	return wrapper;
}

function composer(w: VueWrapper) {
	return w.findComponent(ChatComposer);
}

function texts(w: VueWrapper, selector: string) {
	return w.findAll(selector).map((el) => el.text());
}

beforeEach(() => {
	setActivePinia(createPinia());
	localStorage.clear();
	FakeWebSocket.instances = [];
	vi.stubGlobal("WebSocket", FakeWebSocket);
	Element.prototype.scrollIntoView = vi.fn();
	server = {
		agents: [
			{ name: "bot", model: "model-x" },
			{ name: "other", model: "model-y" },
		],
		sessions: {
			bot: [session("s-other"), session("s-main", { name: "main" })],
			other: [session("o-main", { agent_id: "other", name: "main" })],
		},
		messages: {},
		turns: {},
		fail: new Set(),
	};
	fake.streamAgent.mockReset();
	fake.callTool.mockReset();
	fake.callTool.mockImplementation(
		async (name: string, args: Record<string, string> = {}) => {
			if (server.fail.has(name)) throw new Error(`${name} failed`);
			switch (name) {
				case "agent_list":
					return JSON.stringify(server.agents);
				case "session_list":
					return JSON.stringify(server.sessions[args.agent] ?? []);
				case "session_create": {
					const sess = session("s-new");
					server.sessions[args.agent].push(sess);
					return JSON.stringify(sess);
				}
				case "session_messages":
					return JSON.stringify(server.messages[args.session_id] ?? []);
				case "session_turn":
					return JSON.stringify(server.turns[args.session_id] ?? null);
				case "session_stop":
				case "session_clear":
					return "ok";
				default:
					throw new Error(`unexpected tool ${name}`);
			}
		},
	);
	router = createRouter({
		history: createMemoryHistory(),
		routes: [
			{ path: "/", component: { render: () => null } },
			{
				path: "/agents/:agent/chat/:sessionId?",
				component: { render: () => null },
			},
			{ path: "/:rest(.*)*", component: { render: () => null } },
		],
	});
});

afterEach(() => {
	wrapper?.unmount();
	wrapper = undefined;
	vi.useRealTimers();
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
	document.body.innerHTML = "";
});

describe("history", () => {
	beforeEach(() => {
		server.messages["s-main"] = [
			{ id: "u1", role: "user", content: "Hello **there**", timestamp: DAY1 },
			{
				id: "a1",
				role: "assistant",
				content: "Hi *you*",
				model: "model-x",
				timestamp: DAY1,
			},
			{ id: "sys", role: "system", content: "hidden prompt" },
			{
				id: "t1",
				role: "tool",
				content: '{"id":"call-1","name":"web_search","args":{"q":"a"}}',
			},
			{
				id: "t1b",
				role: "tool",
				content:
					'{"id":"call-1","name":"web_search","args":{"q":"a"},"result":"found"}',
			},
			{ id: "t2", role: "assistant", content: "[tool] legacy_tool" },
			{
				id: "u2",
				role: "user",
				content: "Look",
				timestamp: DAY2,
				media_urls: [
					"https://x/one.png",
					"https://x/two.jpg",
					"https://x/clip.mp4",
					"https://x/song.mp3",
					"https://x/report.pdf",
					"data:text/plain;name=notes.txt;base64,aGk=",
					"file:///tmp/voice.ogg",
					"file:///tmp/doc.txt",
				],
				workflows: [
					{ task_id: "bot/triage", name: "Triage", job_id: "j1" },
					{ task_id: "bot", name: "Sort" },
				],
			},
			{ id: "e1", role: "assistant", content: "Error: quota", timestamp: DAY2 },
		];
	});

	it("renders messages, tools, media, workflows and errors", async () => {
		localStorage.setItem("aviary_token", "tok");
		const w = await mountChat();

		expect(w.find('[data-testid="app-layout"]').exists()).toBe(false);
		expect(calls("session_messages")[0][1]).toEqual({
			session_id: "s-main",
			agent: "bot",
		});

		// The system prompt is dropped; one tool call stored twice shows once.
		const tools = w.findAll("chat-tool-call-stub");
		expect(tools.map((t) => t.attributes("name"))).toEqual([
			"web_search",
			"legacy_tool",
		]);
		expect(tools[0].attributes("result")).toBe("found");
		expect(w.text()).not.toContain("hidden prompt");

		const users = w.findAll('[data-testid="chat-user-message"]');
		expect(users).toHaveLength(2);
		expect(users[0].attributes("id")).toBe("message-u1");
		expect(users[0].html()).toContain("<strong>there</strong>");
		expect(w.html()).toContain("<em>you</em>");
		expect(w.text()).toContain("model-x");

		// Two calendar days, two dividers.
		expect(w.findAll(".my-4 span")).toHaveLength(2);

		// Each media kind gets its own element; saved audio plays through
		// the media endpoint and other saved files are dropped.
		const look = users[1];
		expect(look.findAll("img").map((i) => i.attributes("src"))).toEqual([
			"https://x/one.png",
			"https://x/two.jpg",
		]);
		expect(look.find("video").attributes("src")).toBe("https://x/clip.mp4");
		expect(look.findAll("audio").map((a) => a.attributes("src"))).toEqual([
			"https://x/song.mp3",
			"/api/media?path=%2Ftmp%2Fvoice.ogg&token=tok",
		]);
		const files = look.findAll('a[target="_blank"]');
		expect(files.map((a) => a.text())).toEqual([
			"Open attachment",
			"notes.txt",
		]);
		expect(files[1].attributes("download")).toBe("notes.txt");
		expect(look.html()).not.toContain("doc.txt");

		const links = w.findAll('[data-testid="chat-message-workflow-link"]');
		expect(links.map((l) => l.attributes("href"))).toEqual([
			"/agents/bot/tasks/triage/runs/j1",
			"/agents/bot/tasks/Sort",
		]);

		// The failed reply hangs off the message it failed.
		const error = look.find('[data-testid="chat-message-error"]');
		expect(error.text()).toContain("Error: quota");
		expect(w.findAll('[data-testid="chat-message-bubble"]')).toHaveLength(3);

		expect(composer(w).props("history")).toEqual(["Hello **there**", "Look"]);
		expect(composer(w).props("disabled")).toBe(false);
	});

	it("expands a message's images into the lightbox", async () => {
		const w = await mountChat();
		await w.findAll("button.cursor-zoom-in")[1].trigger("click");
		const preview = document.querySelector('[data-testid="image-preview"]');
		expect(
			preview
				?.querySelector('[data-testid="image-preview-img"]')
				?.getAttribute("src"),
		).toBe("https://x/two.jpg");
		expect(
			preview?.querySelector('[data-testid="image-preview-position"]')
				?.textContent,
		).toContain("2 / 2");
	});

	it("scrolls a deep-linked message into view once", async () => {
		await mountChat({ agent: "bot" }, "/x#message-u2");
		expect(Element.prototype.scrollIntoView).toHaveBeenCalledTimes(1);
		await ws().emit({
			type: "session_message",
			agent_id: "bot",
			session_id: "s-main",
		});
		expect(Element.prototype.scrollIntoView).toHaveBeenCalledTimes(1);
	});

	it("keeps messages when a reload fails", async () => {
		const w = await mountChat();
		const err = vi.spyOn(console, "error").mockImplementation(() => {});
		server.fail.add("session_messages");
		server.fail.add("session_turn");
		document.dispatchEvent(new Event("visibilitychange"));
		await settle();
		expect(err).toHaveBeenCalledWith(
			"Failed to load session messages",
			expect.any(Error),
		);
		expect(w.findAll('[data-testid="chat-user-message"]')).toHaveLength(2);
	});
});

describe("chat picker", () => {
	it("lists chats and switches between them", async () => {
		server.sessions.bot[0].title = "Side chat";
		server.messages["s-other"] = [
			{ id: "x", role: "user", content: "side question" },
		];
		const w = await mountChat();
		const picker = w.find('[data-testid="chat-picker"]');
		expect(picker.find("button").text()).toBe("Main Chat");
		const items = w.findAll('[data-testid="chat-menu-item"]');
		expect(items.map((i) => i.text())).toEqual(["Side chat", "Main Chat"]);
		expect(w.text()).toContain("No messages yet");

		await items[0].trigger("click");
		await settle();
		expect(picker.find("button").text()).toBe("Side chat");
		expect(w.text()).toContain("side question");
		// Embedded chats keep the address bar alone.
		expect(router.currentRoute.value.fullPath).toBe("/");

		// Picking the open chat again does nothing.
		const loads = calls("session_messages").length;
		await items[0].trigger("click");
		await settle();
		expect(calls("session_messages")).toHaveLength(loads);
	});

	it("creates a new chat, and logs a failed create", async () => {
		const w = await mountChat();
		await w.find('button[aria-label="New chat"]').trigger("click");
		await settle();
		expect(w.find('[data-testid="chat-picker"] button').text()).toBe(
			"New chat",
		);
		expect(w.findAll('[data-testid="chat-menu-item"]')).toHaveLength(3);

		const err = vi.spyOn(console, "error").mockImplementation(() => {});
		server.fail.add("session_create");
		await w.find('button[aria-label="New chat"]').trigger("click");
		await settle();
		expect(err).toHaveBeenCalledWith(
			"Failed to create session",
			expect.any(Error),
		);
	});

	it("falls back to the first chat, and empties on a failed list", async () => {
		server.sessions.bot = [session("only")];
		const w = await mountChat();
		expect(calls("session_messages")[0][1].session_id).toBe("only");
		w.unmount();

		setActivePinia(createPinia());
		const err = vi.spyOn(console, "error").mockImplementation(() => {});
		server.fail.add("session_list");
		const failed = await mountChat();
		expect(err).toHaveBeenCalledWith(
			"Failed to load sessions",
			expect.any(Error),
		);
		expect(composer(failed).props("disabled")).toBe(true);
		expect(failed.find('[data-testid="chat-picker"] button').text()).toBe(
			"Select a chat",
		);
	});

	it("restores each chat's draft and empties a cleared chat", async () => {
		server.messages["s-main"] = [{ id: "m", role: "user", content: "kept?" }];
		const w = await mountChat();
		composer(w).vm.$emit("update:modelValue", "half typed");
		await settle();
		const key = chatDraftKey("bot", "s-main");
		expect(getChatDraft(key).text).toBe("half typed");

		await w.findAll('[data-testid="chat-menu-item"]')[0].trigger("click");
		await settle();
		expect(composer(w).props("modelValue")).toBe("");
		await w.findAll('[data-testid="chat-menu-item"]')[1].trigger("click");
		await settle();
		expect(composer(w).props("modelValue")).toBe("half typed");

		// Clearing another chat leaves this one alone.
		const store = useChatSessionsStore();
		await store.clear("bot", "s-other");
		await settle();
		expect(w.text()).toContain("kept?");
		await store.clear("bot", "s-main");
		await settle();
		expect(w.text()).not.toContain("kept?");
		expect(composer(w).props("modelValue")).toBe("");
	});
});

describe("route chat", () => {
	it("follows the route's agent and chat", async () => {
		server.messages["s-other"] = [{ role: "user", content: "other chat" }];
		const w = await mountChat({}, "/agents/bot/chat/s-other");
		expect(w.find('[data-testid="app-layout"]').exists()).toBe(true);
		expect(w.text()).toContain("other chat");
		expect(router.currentRoute.value.fullPath).toBe("/agents/bot/chat/s-other");

		// Picking a chat puts it in the address bar.
		await w.findAll('[data-testid="chat-menu-item"]')[1].trigger("click");
		await settle();
		expect(router.currentRoute.value.fullPath).toBe("/agents/bot/chat/s-main");

		// A bare chat path is Main Chat; another chat's link selects it.
		await router.push("/agents/bot/chat/s-other");
		await settle();
		expect(w.text()).toContain("other chat");
		await router.push("/agents/bot/chat");
		await settle();
		expect(w.text()).not.toContain("other chat");

		// A chat the list doesn't know yet reloads the list.
		server.sessions.bot.push(session("s-late"));
		server.messages["s-late"] = [{ role: "user", content: "late chat" }];
		await router.push("/agents/bot/chat/s-late");
		await settle();
		expect(w.text()).toContain("late chat");

		// The sidebar's link to another agent switches to it.
		server.messages["o-main"] = [{ role: "user", content: "other agent" }];
		await router.push("/agents/other/chat");
		await settle();
		expect(w.text()).toContain("other agent");
		expect(router.currentRoute.value.fullPath).toBe(
			"/agents/other/chat/o-main",
		);

		// Unknown agents are ignored.
		await router.push("/agents/ghost/chat");
		await settle();
		expect(w.text()).toContain("other agent");
	});

	it("asks for an agent when the route names none", async () => {
		const w = await mountChat({}, "/agents/ghost/chat");
		expect(w.text()).toContain("Pick an agent in the sidebar");
		expect(w.find('[data-testid="chat-picker"]').exists()).toBe(false);
		expect(composer(w).props("disabled")).toBe(true);
	});
});

describe("sending", () => {
	it("streams text, tool calls and media into the live turn", async () => {
		let finish = () => {};
		fake.streamAgent.mockImplementation(
			async (_req, onChunk: (c: string, t?: string) => void) => {
				onChunk("Let me ");
				onChunk("check. [tool] ");
				onChunk('{"name":"read","args":{"p":"}"}}');
				onChunk("[tool] bare_tool\nDone");
				onChunk('{"id":"c1","name":"slow"}', "tool");
				onChunk('{"id":"c1","name":"slow"}', "tool");
				onChunk("", "tool");
				onChunk("https://x/out.png", "media");
				onChunk("[tool] {");
				await new Promise<void>((resolve) => {
					finish = resolve;
				});
				vi.spyOn(Date, "now").mockReturnValue(Date.now() + 7000);
				onChunk('{"id":"c1","name":"slow","done":true,"result":"ok"}', "tool");
				onChunk('{"id":"c1","name":"slow"}', "tool");
				onChunk('"name":"tail"}');
			},
		);
		const w = await mountChat({ agent: "bot", project: "proj" });
		composer(w).vm.$emit("update:modelValue", "  do it  ");
		await settle();
		composer(w).vm.$emit("submit");
		await settle();

		expect(fake.streamAgent.mock.calls[0][0]).toEqual({
			agentName: "bot",
			message: "do it",
			sessionID: "s-main",
			mediaURLs: [],
			project: "proj",
			session: undefined,
			retry: false,
		});
		expect(composer(w).props("modelValue")).toBe("");
		expect(composer(w).props("running")).toBe(true);
		expect(w.find('[data-testid="chat-user-message"]').text()).toContain(
			"do it",
		);
		const live = w.findAll("chat-tool-call-stub");
		expect(live.map((t) => t.attributes("name"))).toEqual([
			"read",
			"bare_tool",
			"slow",
		]);
		expect(live[2].attributes("pending")).toBe("true");
		expect(texts(w, '[data-testid="chat-message-content"]')).toEqual([
			"do it",
			"Let me check.",
			"Done",
		]);
		expect(w.find('img[src="https://x/out.png"]').exists()).toBe(true);
		// A running tool shows instead of the typing bubble.
		expect(w.find('[data-testid="chat-typing"]').exists()).toBe(false);

		// Messages from the socket don't replace a streaming reply.
		await ws().emit({
			type: "session_message",
			agent_id: "bot",
			session_id: "s-main",
		});
		expect(w.findAll("chat-tool-call-stub")).toHaveLength(3);

		server.messages["s-main"] = [
			{ id: "u", role: "user", content: "do it" },
			{ id: "a", role: "assistant", content: "stored reply" },
		];
		const loads = calls("session_messages").length;
		finish();
		await settle();
		expect(w.emitted("turnEnd")).toHaveLength(1);
		expect(calls("session_messages").length).toBe(loads + 1);
		expect(w.text()).toContain("stored reply");
		expect(w.findAll("chat-tool-call-stub")).toHaveLength(0);
	});

	it("reports a slow tool's elapsed time", async () => {
		let finish = () => {};
		fake.streamAgent.mockImplementation(
			async (_req, onChunk: (c: string, t?: string) => void) => {
				onChunk('{"id":"c1","name":"slow"}', "tool");
				await new Promise<void>((resolve) => {
					finish = resolve;
				});
			},
		);
		const start = Date.now();
		const now = vi.spyOn(Date, "now").mockReturnValue(start);
		vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
		const w = await mountChat();
		void (w.vm as unknown as { sendText(t: string): Promise<void> }).sendText(
			"go",
		);
		await settle();
		expect(w.find("chat-tool-call-stub").attributes("elapsed")).toBeUndefined();
		now.mockReturnValue(start + 6500);
		vi.advanceTimersByTime(1000);
		await settle();
		expect(w.find("chat-tool-call-stub").attributes("elapsed")).toBe("6");
		finish();
		await settle();
	});

	it("shows a failed turn's error with a retry", async () => {
		fake.streamAgent.mockRejectedValueOnce(new Error("provider down"));
		const w = await mountChat();
		const chat = w.vm as unknown as { sendText(t: string): Promise<void> };
		await chat.sendText("hello");
		await settle();
		const error = w.find('[data-testid="chat-message-error"]');
		expect(error.text()).toContain("Error: provider down");
		expect(error.text()).toContain("model-x");

		// An inline error stays when the tab comes back or the socket reports
		// the turn done.
		const loads = calls("session_messages").length;
		document.dispatchEvent(new Event("visibilitychange"));
		await ws().emit({
			type: "session_processing",
			agent_id: "bot",
			session_id: "s-main",
			is_processing: false,
		});
		expect(calls("session_messages")).toHaveLength(loads);

		fake.streamAgent.mockImplementationOnce(
			async (_req, onChunk: (c: string) => void) => {
				onChunk("recovered");
			},
		);
		server.messages["s-main"] = [
			{ id: "u", role: "user", content: "hello" },
			{ id: "a", role: "assistant", content: "recovered" },
		];
		await error.find('[data-testid="chat-message-retry"]').trigger("click");
		await settle();
		expect(fake.streamAgent.mock.calls[1][0]).toMatchObject({
			message: "hello",
			retry: true,
		});
		expect(w.find('[data-testid="chat-message-error"]').exists()).toBe(false);
		expect(w.text()).toContain("recovered");
	});

	it("retries a stored message that failed", async () => {
		server.messages["s-main"] = [
			{ role: "user", content: "stored", media_urls: ["https://x/a.png"] },
			{ role: "assistant", content: "Error: once" },
		];
		fake.streamAgent.mockRejectedValueOnce("again");
		const w = await mountChat();
		await w.find('[data-testid="chat-message-retry"]').trigger("click");
		await settle();
		expect(fake.streamAgent.mock.calls[0][0]).toMatchObject({
			message: "stored",
			mediaURLs: ["https://x/a.png"],
			retry: true,
		});
		expect(w.find('[data-testid="chat-message-error"]').text()).toContain(
			"Error: again",
		);
	});

	it("drops a stopped turn's error", async () => {
		fake.streamAgent.mockRejectedValueOnce(new Error("Run Cancelled"));
		const w = await mountChat();
		await (w.vm as unknown as { sendText(t: string): Promise<void> }).sendText(
			"hi",
		);
		await settle();
		expect(w.find('[data-testid="chat-message-error"]').exists()).toBe(false);
		expect(w.emitted("turnEnd")).toHaveLength(1);
	});

	it("ignores empty sends", async () => {
		const w = await mountChat();
		await (w.vm as unknown as { sendText(t: string): Promise<void> }).sendText(
			"   ",
		);
		expect(fake.streamAgent).not.toHaveBeenCalled();
	});

	it("opens a pinned session by name", async () => {
		fake.streamAgent.mockResolvedValue(undefined);
		const w = await mountChat({ agent: "bot", session: "prompt" });
		expect(w.find('[data-testid="chat-picker"]').exists()).toBe(false);
		expect(calls("session_list")).toHaveLength(0);
		await (w.vm as unknown as { sendText(t: string): Promise<void> }).sendText(
			"hi",
		);
		expect(fake.streamAgent.mock.calls[0][0]).toMatchObject({
			sessionID: "",
			session: "prompt",
		});
	});

	it("stops the running turn", async () => {
		const w = await mountChat();
		composer(w).vm.$emit("stop");
		await settle();
		expect(calls("session_stop")[0][1]).toEqual({
			agent: "bot",
			session_id: "s-main",
		});

		const err = vi.spyOn(console, "error").mockImplementation(() => {});
		server.fail.add("session_stop");
		composer(w).vm.$emit("stop");
		await settle();
		expect(err).toHaveBeenCalledWith(
			"Failed to stop session",
			expect.any(Error),
		);
	});
});

describe("server turn", () => {
	it("shows a running turn and loads its reply when it ends", async () => {
		server.messages["s-main"] = [
			{
				id: "t",
				role: "tool",
				content: '{"id":"stored","name":"stored_tool","result":"r"}',
			},
		];
		server.turns["s-main"] = {
			processing: true,
			items: [
				{ tool: { id: "stored", name: "stored_tool" } },
				{ tool: { id: "live", name: "live_tool" } },
				{ tool: { name: "done_tool", done: true } },
				{ media_url: "https://x/live.png" },
				{ text: "partial answer" },
				{},
			],
		};
		const w = await mountChat();
		const tools = w.findAll("chat-tool-call-stub");
		expect(tools.map((t) => t.attributes("name"))).toEqual([
			"stored_tool",
			"live_tool",
			"done_tool",
		]);
		expect(tools[1].attributes("pending")).toBe("true");
		expect(tools[2].attributes("pending")).toBe("false");
		expect(w.find('img[src="https://x/live.png"]').exists()).toBe(true);
		expect(w.text()).toContain("partial answer");
		expect(composer(w).props("running")).toBe(true);

		// Updates for other agents, chats without an ID, and other chats.
		await ws().emit({
			type: "session_turn",
			agent_id: "other",
			turn: { session_id: "s-main", processing: false },
		});
		await ws().emit({ type: "session_turn", agent_id: "bot", turn: {} });
		await ws().emit({
			type: "session_turn",
			agent_id: "agent_bot",
			turn: { session_id: "s-other", processing: true },
		});
		expect(w.text()).toContain("partial answer");

		// A newer partial turn replaces the shown one.
		await ws().emit({
			type: "session_turn",
			agent_id: "bot",
			turn: {
				session_id: "s-main",
				processing: true,
				items: [{ text: "more" }],
			},
		});
		expect(w.text()).toContain("more");
		expect(w.text()).not.toContain("partial answer");

		server.turns["s-main"] = null;
		server.messages["s-main"].push({
			id: "a",
			role: "assistant",
			content: "final answer",
		});
		await ws().emit({
			type: "session_turn",
			agent_id: "bot",
			turn: { session_id: "s-main", processing: false },
		});
		expect(w.text()).toContain("final answer");
		expect(w.text()).not.toContain("more");
		expect(composer(w).props("running")).toBe(false);
	});

	it("ignores a turn the server reports in an unexpected shape", async () => {
		server.turns["s-main"] = [1, 2];
		const w = await mountChat();
		expect(composer(w).props("running")).toBe(false);
	});
});

describe("session socket", () => {
	it("tracks processing and reloads on new messages", async () => {
		localStorage.setItem("aviary_token", "a b");
		server.messages["s-main"] = [{ role: "user", content: "question" }];
		const w = await mountChat();
		expect(ws().url).toBe(`ws://${location.host}/api/ws?token=a%20b`);

		await ws().emit({
			type: "session_processing",
			agent_id: "bot",
			session_id: "s-main",
			is_processing: true,
		});
		expect(composer(w).props("running")).toBe(true);
		expect(w.find('[data-testid="chat-typing"]').exists()).toBe(true);

		server.messages["s-main"].push({ role: "assistant", content: "answer" });
		await ws().emit({
			type: "session_processing",
			agent_id: "bot",
			session_id: "s-main",
			is_processing: false,
		});
		expect(composer(w).props("running")).toBe(false);
		expect(w.text()).toContain("answer");

		// Frames for other agents or chats, and malformed frames, change nothing.
		const loads = calls("session_messages").length;
		await ws().emit({
			type: "session_processing",
			agent_id: "other",
			session_id: "s-main",
			is_processing: true,
		});
		await ws().emit({ type: "session_message", agent_id: "other" });
		await ws().emit({
			type: "session_message",
			agent_id: "bot",
			session_id: "s-other",
		});
		await ws().emit({ type: "unknown" });
		await ws().emit("{not json");
		expect(calls("session_messages")).toHaveLength(loads);
		expect(composer(w).props("running")).toBe(false);

		// A new message reloads the chat, and the Chats list once the burst settles.
		server.messages["s-main"].push({ role: "user", content: "follow-up" });
		const lists = calls("session_list").length;
		await ws().emit({
			type: "session_message",
			agent_id: "bot",
			session_id: "s-main",
		});
		expect(w.text()).toContain("follow-up");
		await new Promise((r) => setTimeout(r, 350));
		await settle();
		expect(calls("session_list").length).toBeGreaterThan(lists);
	});

	it("reconnects and catches up after a drop", async () => {
		vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
		const w = await mountChat();
		const first = ws();
		first.onopen?.();
		first.onclose?.();
		vi.advanceTimersByTime(2000);
		expect(FakeWebSocket.instances).toHaveLength(2);

		server.messages["s-main"] = [{ role: "user", content: "missed" }];
		ws().onopen?.();
		await settle();
		expect(w.text()).toContain("missed");

		// A stale socket closing doesn't reconnect.
		first.onclose?.();
		vi.advanceTimersByTime(2000);
		expect(FakeWebSocket.instances).toHaveLength(2);

		w.unmount();
		wrapper = undefined;
		expect(ws().closed).toBe(true);
	});

	it("leaves a chat removed in another tab for Main Chat", async () => {
		server.messages["s-main"] = [{ role: "user", content: "main question" }];
		server.messages["s-other"] = [{ role: "user", content: "other question" }];
		const w = await mountChat({}, "/agents/bot/chat/s-other");
		expect(w.text()).toContain("other question");
		const store = useChatSessionsStore();

		// Another agent's removal and a chat that isn't open change nothing here.
		await ws().emit({
			type: "session_removed",
			agent_id: "other",
			session_id: "s-other",
		});
		expect(router.currentRoute.value.path).toBe("/agents/bot/chat/s-other");

		server.sessions.bot = server.sessions.bot.filter((s) => s.id !== "s-other");
		await ws().emit({
			type: "session_removed",
			agent_id: "bot",
			session_id: "s-other",
		});
		expect(store.sessions("bot").map((s) => s.id)).toEqual(["s-main"]);
		expect(router.currentRoute.value.path).toBe("/agents/bot/chat/s-main");
		expect(w.text()).toContain("main question");
		expect(w.text()).not.toContain("other question");
	});

	it("forgets a removed pinned chat's messages", async () => {
		server.messages.prompt = [{ role: "user", content: "pinned" }];
		const w = await mountChat({ agent: "bot", session: "prompt" });
		expect(w.text()).toContain("pinned");
		await ws().emit({
			type: "session_removed",
			agent_id: "bot",
			session_id: "prompt",
		});
		expect(w.text()).not.toContain("pinned");
	});

	it("clears the running state when a send to a removed chat fails", async () => {
		const w = await mountChat();
		fake.streamAgent.mockImplementationOnce(async () => {
			// The chat is gone, so the server lists it no more.
			server.sessions.bot = server.sessions.bot.filter(
				(s) => s.id !== "s-main",
			);
			throw new Error('session "s-main" not found for agent "bot"');
		});
		await (w.vm as unknown as { sendText(t: string): Promise<void> }).sendText(
			"into the void",
		);
		await settle();
		expect(w.find('[data-testid="chat-message-error"]').text()).toContain(
			"not found",
		);
		expect(composer(w).props("running")).toBe(false);
		expect(w.find('[data-testid="chat-typing"]').exists()).toBe(false);
	});

	it("ignores session events when no chat is selected", async () => {
		server.sessions.bot = [];
		await mountChat();
		await ws().emit({
			type: "session_message",
			agent_id: "bot",
			session_id: "x",
		});
		expect(calls("session_messages")).toHaveLength(0);
	});
});
